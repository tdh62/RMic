package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/gen2brain/malgo"
)

type endpoint struct {
	device       *malgo.Device
	pin          runtime.Pinner
	id           malgo.DeviceID
	name         string
	peerID       string
	peerName     string
	loopback     bool
	pulseID      string
	pulseIndex   uint32
	enabled      bool
	gain         atomic.Uint64
	muted        atomic.Bool
	fault        atomic.Pointer[deviceFault]
	lastCallback atomic.Int64
	releasing    atomic.Bool
	configured   bool
	idleSince    time.Time
}

type deviceFault struct {
	message     string
	recoverable bool
}

func (e *endpoint) fail(message string) {
	e.fault.Store(&deviceFault{message: message})
}

// interrupt marks a transient device loss; explicit safety pauses remain terminal.
func (e *endpoint) interrupt(message string) {
	e.fault.CompareAndSwap(nil, &deviceFault{message: message, recoverable: true})
}
func (e *endpoint) recovering() bool {
	fault := e.fault.Load()
	return fault != nil && fault.recoverable
}
func (e *endpoint) problem() string {
	if value := e.fault.Load(); value != nil {
		return value.message
	}
	return ""
}
func (e *endpoint) level() float64    { return math.Float64frombits(e.gain.Load()) }
func (e *endpoint) setGain(g float64) { e.gain.Store(math.Float64bits(max(0, min(3, g)))) }

// Engine owns independent, explicitly selected playback and capture devices.
// On Linux we use PulseAudio's explicit device IDs (DONT_MOVE streams), so
// removing a device cannot silently reroute the stream to the default device.
type Engine struct {
	captureChannels             int
	context                     *malgo.AllocatedContext
	pulse                       *pulseBackend
	playback, capture           endpoint
	mu                          sync.RWMutex
	source, captureSink         *Buffer
	stop, done                  chan struct{}
	lifecycle                   sync.Mutex // serializes native open, recovery, idle release and close
	preparing, onDemand, closed bool
	idleTimeout                 time.Duration
}

func NewEngine() (*Engine, error) {
	e := &Engine{captureChannels: 2, idleTimeout: 30 * time.Second, stop: make(chan struct{}), done: make(chan struct{})}
	if runtime.GOOS == "linux" {
		backend, err := newPulseBackend(e)
		if err != nil {
			return nil, fmt.Errorf("connect native audio service: %w", err)
		}
		e.pulse = backend
	} else {
		var backends []malgo.Backend
		if runtime.GOOS == "windows" {
			backends = []malgo.Backend{malgo.BackendWasapi}
		} else if runtime.GOOS == "darwin" {
			backends = []malgo.Backend{malgo.BackendCoreaudio}
		}
		ctx, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
		if err != nil {
			return nil, fmt.Errorf("initialize native audio: %w", err)
		}
		e.context = ctx
	}
	e.playback.setGain(1)
	e.capture.setGain(0.5)
	go e.watch()
	return e, nil
}
func (e *Engine) list(kind malgo.DeviceType) ([]malgo.DeviceInfo, error) {
	return e.context.Devices(kind)
}
func (e *Engine) Devices() []string { return e.names(malgo.Playback) }
func (e *Engine) CaptureDevices() []string {
	names := e.names(malgo.Capture)
	if runtime.GOOS == "windows" {
		for _, name := range e.Devices() {
			names = append(names, LoopbackPrefix+name)
		}
	}
	return names
}
func (e *Engine) MicrophoneDevices() []string {
	if runtime.GOOS == "windows" {
		return WindowsMicrophoneDevices(e.Devices(), e.names(malgo.Capture))
	}
	if runtime.GOOS == "darwin" {
		return MacMicrophoneDevices(e.Devices())
	}
	return e.Devices()
}
func (e *Engine) MicrophonePeer() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.playback.peerName
}
func (e *Engine) names(kind malgo.DeviceType) []string {
	if e.pulse != nil {
		return e.pulse.names(kind == malgo.Capture)
	}
	devices, err := e.list(kind)
	if err != nil {
		return nil
	}
	names := make([]string, len(devices))
	for i := range devices {
		names[i] = devices[i].Name()
	}
	return names
}
func (e *Engine) selectDevice(kind malgo.DeviceType, match string) (malgo.DeviceInfo, error) {
	devices, err := e.list(kind)
	if err != nil {
		return malgo.DeviceInfo{}, err
	}
	var matches []malgo.DeviceInfo
	// Prefer an exact name before considering substrings.
	for _, d := range devices {
		if strings.EqualFold(d.Name(), match) {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 && match != "" {
		for _, d := range devices {
			if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(match)) {
				matches = append(matches, d)
			}
		}
	}
	if match == "" || len(matches) != 1 {
		return malgo.DeviceInfo{}, fmt.Errorf("device %q matched %d endpoints; use --list-devices and a unique name", match, len(matches))
	}
	return matches[0], nil
}
func (e *Engine) Start(match string) error {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	return e.start(malgo.Playback, match, &e.playback)
}
func (e *Engine) StartCapture(match string) error { return e.StartCaptureChannels(match, 2) }
func (e *Engine) StartCaptureChannels(match string, channels int) error {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if err := ValidateChannels(channels); err != nil {
		return err
	}
	e.captureChannels = channels
	return e.start(malgo.Capture, match, &e.capture)
}
func (e *Engine) CaptureChannels() int { return e.captureChannels }
func (e *Engine) writeCapture(sink *Buffer, pcm []int16) {
	if sink.Channels() == e.captureChannels {
		sink.Write(pcm)
		return
	}
	if sink.Channels() == 2 {
		sink.Write(Stereo(pcm, e.captureChannels))
	}
}
func (e *Engine) start(kind malgo.DeviceType, match string, target *endpoint) error {
	if e.pulse != nil {
		return e.pulse.start(kind == malgo.Capture, match)
	}
	loopback := false
	selectKind := kind
	if kind == malgo.Capture && strings.HasPrefix(match, LoopbackPrefix) {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("WASAPI loopback capture is only available on Windows")
		}
		match = strings.TrimPrefix(match, LoopbackPrefix)
		loopback = true
		selectKind = malgo.Playback
	}
	selected, err := e.selectDevice(selectKind, match)
	if err != nil {
		return err
	}
	return e.startSelected(kind, selected, loopback, target)
}

// startSelected also serves recovery, which must use the pinned identity rather
// than resolving a possibly ambiguous or reused display name again.
func (e *Engine) startSelected(kind malgo.DeviceType, selected malgo.DeviceInfo, loopback bool, target *endpoint) error {
	match := selected.Name()
	selectKind := kind
	if loopback {
		selectKind = malgo.Playback
	}
	var err error
	if runtime.GOOS == "darwin" && kind == malgo.Playback && !IsMacCable(selected.Name()) {
		return fmt.Errorf("macOS 手机麦克风需写入 BlackHole 虚拟设备；请先安装 BlackHole 并选择其输出端")
	}
	peerName, peerID := "", ""
	if runtime.GOOS == "windows" && kind == malgo.Playback {
		peerName, err = WindowsMicrophonePeer(selected.Name(), e.names(malgo.Capture))
		if err != nil {
			return err
		}
		peer, peerErr := e.selectDevice(malgo.Capture, peerName)
		if peerErr != nil {
			return peerErr
		}
		peerID = wasapiEndpointID(peer.ID)
		if target.configured && target.peerID != "" && peerID != target.peerID {
			return fmt.Errorf("虚拟麦克风的配对录音端身份已变化，请在电脑重新选择设备")
		}
		if err = checkMicrophoneListening(peerID, peerName); err != nil {
			return err
		}
	}
	if kind == malgo.Capture && e.captureChannels > 2 {
		info, infoErr := e.context.DeviceInfo(selectKind, selected.ID, malgo.Shared)
		if infoErr != nil {
			return fmt.Errorf("读取多声道设备格式：%w", infoErr)
		}
		supported := false
		for _, format := range info.Formats {
			if int(format.Channels) == e.captureChannels {
				supported = true
			}
		}
		if !supported {
			return fmt.Errorf("所选设备未报告 %d 声道原生格式；请检查系统音频设备声道布局或选择其他设备", e.captureChannels)
		}
	}
	if e.preparing {
		e.mu.Lock()
		target.id, target.name = selected.ID, selected.Name()
		target.peerName, target.peerID = peerName, peerID
		target.loopback, target.configured = loopback, true
		if loopback {
			target.name = LoopbackPrefix + target.name
		}
		e.mu.Unlock()
		return nil
	}
	id := selected.ID
	target.pin.Pin(&id[0])
	deviceKind := kind
	if loopback {
		deviceKind = malgo.Loopback
	}
	config := malgo.DefaultDeviceConfig(deviceKind)
	config.SampleRate = SampleRate
	config.PeriodSizeInMilliseconds = 10
	config.Periods = 3
	config.Wasapi.NoAutoStreamRouting = 1
	if kind == malgo.Playback {
		config.Playback.Format = malgo.FormatS16
		config.Playback.Channels = Channels
		config.Playback.DeviceID = unsafe.Pointer(&id[0])
	} else {
		config.Capture.Format = malgo.FormatS16
		config.Capture.Channels = uint32(e.captureChannels)
		config.Capture.DeviceID = unsafe.Pointer(&id[0])
	}
	callbacks := malgo.DeviceCallbacks{Data: func(out, in []byte, frames uint32) {
		target.lastCallback.Store(time.Now().UnixNano())
		e.mu.RLock()
		source, sink := e.source, e.captureSink
		e.mu.RUnlock()
		if kind == malgo.Playback {
			if source == nil || target.problem() != "" {
				clear(out)
				return
			}
			source.Read(out, target.level(), target.muted.Load())
		} else if sink != nil && target.problem() == "" {
			// Copy out of the native callback's transient memory. The bounded queue
			// keeps a delayed encoder from accumulating audio indefinitely.
			pcm := make([]int16, len(in)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(in[i*2:]))
			}
			e.writeCapture(sink, pcm)
		}
	}, Stop: func() {
		if !target.releasing.Load() {
			target.interrupt("音频设备已停止，正在等待原设备恢复")
		}
	}}
	dev, err := malgo.InitDevice(e.context.Context, config, callbacks)
	if err != nil {
		target.pin.Unpin()
		if runtime.GOOS == "windows" && kind == malgo.Playback && steamMicrophone(selected.Name()) {
			return fmt.Errorf("open %q: %w；请确认 Steam 虚拟麦克风的播放/录音端均已启用；若为格式错误，检查两端的双声道 48000 Hz PCM 格式（部分版本需 32 位 PCM）", match, err)
		}
		if runtime.GOOS == "darwin" && kind == malgo.Capture {
			return fmt.Errorf("open %q: %w；请检查 CoreAudio 设备是否可用，并在系统设置 → 隐私与安全性 → 麦克风中允许启动 RMic 的终端", match, err)
		}
		return fmt.Errorf("open %q: %w", match, err)
	}
	target.lastCallback.Store(time.Now().UnixNano())
	if err = dev.Start(); err != nil {
		dev.Uninit()
		target.pin.Unpin()
		if runtime.GOOS == "darwin" && kind == malgo.Capture {
			return fmt.Errorf("start %q: %w；请检查系统麦克风权限和 CoreAudio 设备格式", match, err)
		}
		return fmt.Errorf("start %q: %w", match, err)
	}
	e.mu.Lock()
	target.device = dev
	target.enabled = true
	target.configured = true
	target.id = selected.ID
	target.name = selected.Name()
	target.peerName = peerName
	target.peerID = peerID
	target.loopback = loopback
	if loopback {
		target.name = LoopbackPrefix + target.name
	}
	e.mu.Unlock()
	return nil
}
func (e *Engine) Name() string               { e.mu.RLock(); defer e.mu.RUnlock(); return e.playback.name }
func (e *Engine) CaptureName() string        { e.mu.RLock(); defer e.mu.RUnlock(); return e.capture.name }
func (e *Engine) SetSource(b *Buffer)        { e.setDemand(false, b) }
func (e *Engine) SetCaptureSink(b *Buffer)   { e.setDemand(true, b) }
func (e *Engine) Gain() float64              { return e.playback.level() }
func (e *Engine) SetGain(g float64)          { e.playback.setGain(g) }
func (e *Engine) Muted() bool                { return e.playback.muted.Load() }
func (e *Engine) ToggleMute()                { e.playback.muted.Store(!e.playback.muted.Load()) }
func (e *Engine) CaptureGain() float64       { return e.capture.level() }
func (e *Engine) SetCaptureGain(g float64)   { e.capture.setGain(g) }
func (e *Engine) CaptureMuted() bool         { return e.capture.muted.Load() }
func (e *Engine) ToggleCaptureMute()         { e.capture.muted.Store(!e.capture.muted.Load()) }
func (e *Engine) OutputError() string        { return e.playback.problem() }
func (e *Engine) CaptureError() string       { return e.capture.problem() }
func (e *Engine) PauseCapture(reason string) { e.capture.fail(reason) }

// Pulse IDs are endpoint names; other backends use opaque IDs and must not be
// treated as PulseAudio routing identities.
func (e *Engine) PulseRoutes() (string, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.playback.pulseID, e.capture.pulseID
}
func (e *Engine) watch() {
	defer close(e.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-ticker.C:
			e.lifecycle.Lock()
			for _, item := range []struct {
				kind malgo.DeviceType
				ep   *endpoint
			}{{malgo.Playback, &e.playback}, {malgo.Capture, &e.capture}} {
				if e.releaseIdle(item.ep, time.Now()) {
					continue
				}
				e.mu.RLock()
				enabled := item.ep.enabled
				id := item.ep.id
				loopback, peerID, peerName := item.ep.loopback, item.ep.peerID, item.ep.peerName
				pulseID, pulseIndex := item.ep.pulseID, item.ep.pulseIndex
				e.mu.RUnlock()
				if !enabled {
					continue
				}
				if item.ep.problem() != "" {
					if e.pulse == nil && item.ep.recovering() {
						e.recoverDevice(item.kind, item.ep)
					}
					continue
				}
				found := false
				if e.pulse != nil {
					found = e.pulse.exists(item.kind == malgo.Capture, pulseID, pulseIndex)
				} else {
					kind := item.kind
					if loopback {
						kind = malgo.Playback
					}
					devices, err := e.list(kind)
					if err == nil {
						for _, d := range devices {
							if sameDeviceID(d.ID, id) {
								found = true
								break
							}
						}
					}
				}
				if !found {
					if e.pulse != nil {
						item.ep.fail("所选音频设备已失效；此方向已暂停，请检查设备并重启 RMic")
					} else {
						item.ep.interrupt("所选音频设备暂不可用，正在等待原设备恢复")
					}
					continue
				}
				if peerID != "" {
					peers, err := e.list(malgo.Capture)
					present := false
					if err == nil {
						for _, peer := range peers {
							if wasapiEndpointID(peer.ID) == peerID {
								present = true
								break
							}
						}
					}
					if !present {
						item.ep.interrupt("虚拟麦克风的配对录音端暂不可用，正在等待 " + peerName + " 恢复")
						continue
					}
					if err := checkMicrophoneListening(peerID, peerName); err != nil {
						item.ep.fail(err.Error())
						continue
					}
				}
				// A silent WASAPI render endpoint legitimately produces no
				// loopback callbacks. Presence and Stop still detect loss.
				if !loopback && time.Since(time.Unix(0, item.ep.lastCallback.Load())) > 3*time.Second {
					if e.pulse != nil {
						item.ep.fail("音频设备超过 3 秒没有响应；此方向已暂停，请重启 RMic")
					} else {
						item.ep.interrupt("音频设备超过 3 秒没有响应，正在重新打开原设备")
					}
				}
			}
			e.lifecycle.Unlock()
		}
	}
}

// Only watch owns recovery. Close joins watch before destroying native handles.
// Enumerate by identity first: an absent device must never select the default or
// a different endpoint that happens to have the same display name.
func (e *Engine) recoverDevice(kind malgo.DeviceType, ep *endpoint) {
	fault := ep.fault.Load()
	if fault == nil || !fault.recoverable {
		return
	}
	selectKind := kind
	if ep.loopback {
		selectKind = malgo.Playback
	}
	devices, err := e.list(selectKind)
	if err != nil {
		return
	}
	for _, selected := range devices {
		if !sameDeviceID(selected.ID, ep.id) {
			continue
		}
		if ep.device != nil {
			ep.device.Uninit()
			ep.device = nil
			ep.pin.Unpin()
		}
		if !ep.fault.CompareAndSwap(fault, nil) {
			return // A local safety check paused this direction during recovery.
		}
		if err := e.startSelected(kind, selected, ep.loopback, ep); err != nil {
			ep.interrupt("正在等待原音频设备恢复：" + err.Error())
		}
		return
	}
}

func sameDeviceID(a, b malgo.DeviceID) bool {
	if runtime.GOOS == "windows" {
		id := wasapiEndpointID(a)
		return id != "" && id == wasapiEndpointID(b)
	}
	if runtime.GOOS == "darwin" {
		return sameCoreAudioID(a, b)
	}
	return a == b
}

func wasapiEndpointID(id malgo.DeviceID) string {
	var value []uint16
	for offset := 0; offset+1 < len(id); offset += 2 {
		unit := binary.LittleEndian.Uint16(id[offset:])
		if unit == 0 {
			break
		}
		value = append(value, unit)
	}
	return string(utf16.Decode(value))
}
func checkMicrophoneListening(id, name string) error {
	listening, err := microphoneListening(id)
	if err != nil {
		return err
	}
	if listening {
		return fmt.Errorf("%s 已开启 Windows 侦听，会从电脑播放手机声音；请在声音控制面板 → 录音 → 此设备属性 → 侦听中取消“侦听此设备”，然后重新启动传音", name)
	}
	return nil
}

// CheckWindowsRoutes re-enumerates the pinned endpoints, so a rename, unplug or
// newly selected default cannot silently change the inspected route.
func (e *Engine) CheckWindowsRoutes() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows route checks require WASAPI")
	}
	e.mu.RLock()
	outputID, captureID, loopback := e.playback.id, e.capture.id, e.capture.loopback
	e.mu.RUnlock()
	output, capture := "", ""
	outputs, err := e.list(malgo.Playback)
	if err != nil {
		return err
	}
	for _, device := range outputs {
		if sameDeviceID(device.ID, outputID) {
			output = device.Name()
		}
		if loopback && sameDeviceID(device.ID, captureID) {
			capture = LoopbackPrefix + device.Name()
		}
	}
	if !loopback {
		captures, err := e.list(malgo.Capture)
		if err != nil {
			return err
		}
		for _, device := range captures {
			if sameDeviceID(device.ID, captureID) {
				capture = device.Name()
			}
		}
	}
	if output == "" || capture == "" {
		return ErrWindowsEndpointMissing
	}
	if loopback && sameDeviceID(outputID, captureID) {
		return fmt.Errorf("不能回环采集手机麦克风写入的同一个播放端点")
	}
	return ValidateWindowsDuplex(output, capture)
}

func (e *Engine) Close() {
	e.lifecycle.Lock()
	if e.closed {
		e.lifecycle.Unlock()
		return
	}
	e.closed = true
	e.lifecycle.Unlock()
	close(e.stop)
	<-e.done
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if e.pulse != nil {
		e.pulse.close()
		return
	}
	for _, ep := range []*endpoint{&e.capture, &e.playback} {
		if ep.device != nil {
			ep.device.Uninit()
			ep.pin.Unpin()
		}
	}
	_ = e.context.Uninit()
	e.context.Free()
}
