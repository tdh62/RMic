package audio

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// pulseBackend uses the native PulseAudio protocol directly. Explicit NoMove
// streams and stream-killed events keep device loss bounded and prevent fallback.
// No external recording process or extra native library is needed.
type pulseBackend struct {
	engine                       *Engine
	discovery                    *pulse.Client
	wire                         *proto.Client
	conn                         net.Conn
	mu                           sync.Mutex
	playbackID, recordID         uint32
	playbackActive, recordActive bool
	playbackGeneration           uint64
	requested                    atomic.Int64
	notify                       chan struct{}
	done                         chan struct{}
}

func newPulseBackend(e *Engine) (*pulseBackend, error) {
	server := os.Getenv("PULSE_SERVER")
	if server == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		if machine, err := os.ReadFile("/etc/machine-id"); err == nil {
			if config, err := os.UserConfigDir(); err == nil {
				candidate := filepath.Join(config, "pulse", strings.TrimSpace(string(machine))+"-runtime", "native")
				if _, err := os.Stat(candidate); err == nil {
					server = candidate
				}
			}
		}
	}
	discovery, err := pulse.NewClient(pulse.ClientServerString(server), pulse.ClientApplicationName("RMic"), pulse.ClientTimeout(2*time.Second))
	if err != nil {
		return nil, err
	}
	wire, conn, err := proto.Connect(server)
	if err != nil {
		discovery.Close()
		return nil, err
	}
	wire.SetTimeout(2 * time.Second)
	p := &pulseBackend{engine: e, discovery: discovery, wire: wire, conn: conn, notify: make(chan struct{}, 1), done: make(chan struct{})}
	wire.Callback = p.message
	if err = wire.Request(&proto.SetClientName{Props: proto.PropList{"application.name": proto.PropListString("RMic audio")}}, &proto.SetClientNameReply{}); err != nil {
		_ = conn.Close()
		discovery.Close()
		return nil, err
	}
	go p.playbackLoop()
	return p, nil
}
func (p *pulseBackend) names(capture bool) []string {
	var names []string
	if capture {
		if sources, err := p.discovery.ListSources(); err == nil {
			for _, source := range sources {
				names = append(names, source.Name())
			}
		}
	} else {
		if sinks, err := p.discovery.ListSinks(); err == nil {
			for _, sink := range sinks {
				names = append(names, sink.Name())
			}
		}
	}
	return names
}
func chooseName(match string, names []string) (string, error) {
	var exact, partial []string
	for _, name := range names {
		if strings.EqualFold(name, match) {
			exact = append(exact, name)
		}
		if match != "" && strings.Contains(strings.ToLower(name), strings.ToLower(match)) {
			partial = append(partial, name)
		}
	}
	if len(exact) > 0 {
		partial = exact
	}
	if match == "" || len(partial) != 1 {
		return "", fmt.Errorf("device %q matched %d endpoints; use --list-devices and a unique name", match, len(partial))
	}
	return partial[0], nil
}
func (p *pulseBackend) start(capture bool, match string) error {
	name, err := chooseName(match, p.names(capture))
	if err != nil {
		return err
	}
	e := p.engine
	spec := proto.SampleSpec{Format: proto.FormatInt16LE, Channels: Channels, Rate: SampleRate}
	channels := proto.ChannelMap{proto.ChannelLeft, proto.ChannelRight}
	var id string
	var index uint32
	if capture {
		count := e.captureChannels
		spec.Channels = byte(count)
		if count > 2 {
			channels = proto.ChannelMap{proto.ChannelFrontLeft, proto.ChannelFrontRight, proto.ChannelFrontCenter, proto.ChannelLFE}
			if count == 8 {
				channels = append(channels, proto.ChannelRearLeft, proto.ChannelRearRight)
			}
			channels = append(channels, proto.ChannelLeftSide, proto.ChannelRightSide)
		}
		sources, err := p.discovery.ListSources()
		if err != nil {
			return err
		}
		for _, source := range sources {
			if source.Name() == name {
				if count > 2 {
					have := source.Channels()
					for _, channel := range channels {
						found := false
						for _, actual := range have {
							if actual == channel {
								found = true
							}
						}
						if !found {
							return fmt.Errorf("所选采集设备不提供请求的 %d 声道布局；不会扩展立体声为环绕声", count)
						}
					}
				}
				id = source.ID()
				index = source.SourceIndex()
				break
			}
		}
		if id == "" {
			return fmt.Errorf("selected capture device disappeared")
		}
		if e.capture.configured && (e.capture.pulseID != id || e.capture.pulseIndex != index) {
			return fmt.Errorf("所选采集设备身份已变化，请重新选择")
		}
		if e.preparing {
			e.mu.Lock()
			e.capture.name, e.capture.pulseID, e.capture.pulseIndex = name, id, index
			e.capture.configured = true
			e.mu.Unlock()
			return nil
		}
		reply := proto.CreateRecordStreamReply{}
		request := proto.CreateRecordStream{SampleSpec: spec, ChannelMap: channels, SourceName: id, SourceIndex: proto.Undefined, Corked: true, NoMove: true, NoRemix: count > 2, AdjustLatency: true, BufferMaxLength: uint32(19200 * count), BufferFragSize: uint32(960 * count), DirectOnInputIndex: proto.Undefined, ChannelVolumes: make(proto.ChannelVolumes, count), Properties: proto.PropList{"media.name": proto.PropListString("RMic computer audio")}}
		for i := range request.ChannelVolumes {
			request.ChannelVolumes[i] = 65536
		}
		if err = p.wire.Request(&request, &reply); err != nil {
			return err
		}
		p.mu.Lock()
		p.recordID = reply.StreamIndex
		p.recordActive = true
		p.mu.Unlock()
		e.mu.Lock()
		e.capture.name = name
		e.capture.pulseID = id
		e.capture.pulseIndex = index
		e.capture.enabled = true
		e.mu.Unlock()
		e.capture.lastCallback.Store(time.Now().UnixNano())
		if err = p.wire.Request(&proto.CorkRecordStream{StreamIndex: reply.StreamIndex, Corked: false}, nil); err != nil {
			return err
		}
	} else {
		sinks, err := p.discovery.ListSinks()
		if err != nil {
			return err
		}
		for _, sink := range sinks {
			if sink.Name() == name {
				id = sink.ID()
				index = sink.SinkIndex()
				break
			}
		}
		if id == "" {
			return fmt.Errorf("selected playback device disappeared")
		}
		if e.playback.configured && (e.playback.pulseID != id || e.playback.pulseIndex != index) {
			return fmt.Errorf("所选播放设备身份已变化，请重新选择")
		}
		if e.preparing {
			e.mu.Lock()
			e.playback.name, e.playback.pulseID, e.playback.pulseIndex = name, id, index
			e.playback.configured = true
			e.mu.Unlock()
			return nil
		}
		reply := proto.CreatePlaybackStreamReply{}
		request := proto.CreatePlaybackStream{SampleSpec: spec, ChannelMap: channels, SinkName: id, SinkIndex: proto.Undefined, Corked: true, NoMove: true, BufferMaxLength: 38400, BufferTargetLength: 5760, BufferPrebufferLength: 0, BufferMinimumRequest: 1920, ChannelVolumes: proto.ChannelVolumes{65536, 65536}, VolumeSet: true, Properties: proto.PropList{"media.name": proto.PropListString("RMic phone microphone")}}
		if err = p.wire.Request(&request, &reply); err != nil {
			return err
		}
		p.mu.Lock()
		p.playbackGeneration++
		p.playbackID = reply.StreamIndex
		p.playbackActive = true
		p.mu.Unlock()
		e.mu.Lock()
		e.playback.name = name
		e.playback.pulseID = id
		e.playback.pulseIndex = index
		e.playback.enabled = true
		e.mu.Unlock()
		e.playback.lastCallback.Store(time.Now().UnixNano())
		p.requested.Add(int64(reply.Missing))
		p.signal()
		if err = p.wire.Request(&proto.CorkPlaybackStream{StreamIndex: reply.StreamIndex, Corked: false}, nil); err != nil {
			return err
		}
	}
	if id == "" {
		return fmt.Errorf("selected device disappeared")
	}
	return nil
}
func (p *pulseBackend) signal() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}
func (p *pulseBackend) message(message any) {
	p.mu.Lock()
	play, record := p.playbackID, p.recordID
	playActive, recordActive := p.playbackActive, p.recordActive
	p.mu.Unlock()
	e := p.engine
	switch message := message.(type) {
	case *proto.Request:
		if playActive && message.StreamIndex == play {
			p.requested.Add(int64(message.Length))
			p.signal()
		}
	case *proto.DataPacket:
		if !recordActive || message.StreamIndex != record {
			return
		}
		e.capture.lastCallback.Store(time.Now().UnixNano())
		e.mu.RLock()
		sink := e.captureSink
		e.mu.RUnlock()
		if sink == nil || e.capture.problem() != "" {
			return
		}
		pcm := make([]int16, len(message.Data)/2)
		for i := range pcm {
			pcm[i] = int16(binary.LittleEndian.Uint16(message.Data[i*2:]))
		}
		e.writeCapture(sink, pcm)
	case *proto.RecordStreamKilled:
		if recordActive && message.StreamIndex == record {
			e.capture.fail("所选采集设备已失效；电脑到手机方向已暂停，不会切换到默认设备")
		}
	case *proto.PlaybackStreamKilled:
		if playActive && message.StreamIndex == play {
			e.playback.fail("所选输出设备已失效；手机到电脑方向已暂停，不会切换到默认设备")
		}
	case *proto.ConnectionClosed:
		e.capture.fail("音频服务连接中断；请重启 RMic")
		e.playback.fail("音频服务连接中断；请重启 RMic")
	}
}
func (p *pulseBackend) playbackLoop() {
	defer close(p.done)
	data := make([]byte, 3840)
	for {
		select {
		case <-p.engine.stop:
			return
		case <-p.notify:
			p.mu.Lock()
			stream := p.playbackID
			generation := p.playbackGeneration
			remaining := p.requested.Swap(0)
			p.mu.Unlock()
			for remaining > 0 {
				n := min(int64(len(data)), remaining)
				n -= n % 4
				if n == 0 {
					break
				}
				e := p.engine
				e.mu.RLock()
				source := e.source
				e.mu.RUnlock()
				e.playback.lastCallback.Store(time.Now().UnixNano())
				if source == nil || e.playback.problem() != "" {
					clear(data[:n])
				} else {
					source.Read(data[:n], e.Gain(), e.Muted())
				}
				p.mu.Lock()
				if !p.playbackActive || p.playbackGeneration != generation {
					p.mu.Unlock()
					break
				}
				err := p.wire.Send(stream, data[:n])
				p.mu.Unlock()
				if err != nil {
					e.playback.fail("音频输出失败；此方向已暂停")
					break
				}
				remaining -= n
			}
		}
	}
}
func (p *pulseBackend) exists(capture bool, id string, index uint32) bool {
	if capture {
		source, err := p.discovery.SourceByID(id)
		return err == nil && source.SourceIndex() == index
	}
	sink, err := p.discovery.SinkByID(id)
	return err == nil && sink.SinkIndex() == index
}

// Stop streams rather than merely corking: corked record streams can still
// keep a physical microphone open. Discovery and the virtual topology remain.
func (p *pulseBackend) stopStream(capture bool) {
	p.mu.Lock()
	id, active := p.playbackID, p.playbackActive
	if capture {
		id, active = p.recordID, p.recordActive
		p.recordActive = false
	} else {
		p.playbackActive = false
		p.playbackGeneration++
		p.requested.Store(0)
	}
	p.mu.Unlock()
	if !active {
		return
	}
	var err error
	if capture {
		err = p.wire.Request(&proto.DeleteRecordStream{StreamIndex: id}, nil)
	} else {
		err = p.wire.Request(&proto.DeletePlaybackStream{StreamIndex: id}, nil)
	}
	if err != nil && err != proto.ErrNoSuchEntity && err != proto.ErrEntityKilled {
		// A timed-out delete cannot prove that recording stopped. Closing our
		// audio connection releases every stream; report the failure explicitly.
		p.engine.capture.fail("释放音频设备失败，已关闭音频连接；请重启服务：" + err.Error())
		p.engine.playback.fail("释放音频设备失败，已关闭音频连接；请重启服务：" + err.Error())
		_ = p.conn.Close()
	}
}

func (p *pulseBackend) close() { _ = p.conn.Close(); <-p.done; p.discovery.Close() }
