package audio

import (
	"fmt"
	"time"

	"github.com/gen2brain/malgo"
)

// Prepare pins device identities without opening an audio stream. Discovery
// connections and virtual devices remain available while the service is idle.
func (e *Engine) Prepare(output, capture string, channels int, idle time.Duration) error {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if e.closed {
		return fmt.Errorf("audio engine closed")
	}
	if idle <= 0 {
		return fmt.Errorf("audio idle timeout must be positive")
	}
	if err := ValidateChannels(channels); err != nil {
		return err
	}
	e.onDemand, e.preparing = true, true
	defer func() { e.preparing = false }()
	e.idleTimeout, e.captureChannels = idle, channels
	if output != "" {
		if err := e.start(malgo.Playback, output, &e.playback); err != nil {
			return err
		}
	}
	if capture != "" {
		if err := e.start(malgo.Capture, capture, &e.capture); err != nil {
			return err
		}
	}
	return nil
}

// Ensure methods let an authorized session report an open failure before
// accepting audio. An unbound opened endpoint still expires after idleTimeout.
func (e *Engine) EnsureOutput() error  { return e.ensure(false) }
func (e *Engine) EnsureCapture() error { return e.ensure(true) }
func (e *Engine) ensure(capture bool) error {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	ep := &e.playback
	if capture {
		ep = &e.capture
	}
	return e.openPrepared(ep, capture)
}

func (e *Engine) openPrepared(ep *endpoint, capture bool) error {
	if e.closed {
		return fmt.Errorf("audio engine closed")
	}
	if !e.onDemand {
		return nil
	}
	if !ep.configured {
		return fmt.Errorf("audio direction is not configured")
	}
	if ep.enabled {
		if problem := ep.problem(); problem != "" {
			return fmt.Errorf("%s", problem)
		}
		return nil
	}
	// A safety pause must not be cleared by reconnecting a phone.
	if problem := ep.problem(); problem != "" {
		return fmt.Errorf("%s", problem)
	}
	var err error
	if e.pulse != nil {
		if !e.pulse.exists(capture, ep.pulseID, ep.pulseIndex) {
			err = fmt.Errorf("所选音频设备身份已变化或已移除；请在电脑重新选择设备")
		} else {
			err = e.pulse.start(capture, ep.name)
		}
	} else {
		kind := malgo.Playback
		if capture {
			kind = malgo.Capture
		}
		selectKind := kind
		if ep.loopback {
			selectKind = malgo.Playback
		}
		var devices []malgo.DeviceInfo
		devices, err = e.list(selectKind)
		if err == nil {
			found := false
			for _, device := range devices {
				if sameDeviceID(device.ID, ep.id) {
					found = true
					err = e.startSelected(kind, device, ep.loopback, ep)
					break
				}
			}
			if !found {
				err = fmt.Errorf("所选音频设备已移除；不会使用默认设备，请重新选择设备")
			}
		}
	}
	if err != nil {
		// A failed native open may have allocated a stream before start failed.
		e.releaseEndpoint(ep)
		ep.fail(err.Error())
		return err
	}
	ep.idleSince = time.Now()
	return nil
}

func (e *Engine) setDemand(capture bool, b *Buffer) {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if e.closed {
		return
	}
	ep := &e.playback
	if capture {
		ep = &e.capture
	}
	if b != nil {
		if err := e.openPrepared(ep, capture); err != nil {
			ep.fail(err.Error())
			return
		}
	}
	e.mu.Lock()
	if capture {
		e.captureSink = b
	} else {
		e.source = b
	}
	e.mu.Unlock()
	if b != nil {
		ep.idleSince = time.Time{}
	} else if ep.idleSince.IsZero() {
		ep.idleSince = time.Now()
	}
}

// lifecycle is held by the watcher. Reconnection uses the same lock, so there
// is no stale timer callback capable of closing a newly attached session.
func (e *Engine) releaseIdle(ep *endpoint, now time.Time) bool {
	if !e.onDemand || !ep.enabled || ep.idleSince.IsZero() || now.Sub(ep.idleSince) < e.idleTimeout {
		return false
	}
	e.releaseEndpoint(ep)
	return true
}

func (e *Engine) releaseEndpoint(ep *endpoint) {
	if e.pulse != nil {
		e.pulse.stopStream(ep == &e.capture)
	} else if ep.device != nil {
		ep.releasing.Store(true)
		ep.device.Uninit()
		ep.device = nil
		ep.pin.Unpin()
		ep.releasing.Store(false)
	}
	e.mu.Lock()
	ep.enabled = false
	e.mu.Unlock()
	ep.idleSince = time.Time{}
}

// DeviceState describes resource ownership separately from session state.
func (e *Engine) OutputDeviceState() string  { return e.deviceState(false) }
func (e *Engine) CaptureDeviceState() string { return e.deviceState(true) }
func (e *Engine) deviceState(capture bool) string {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	ep := &e.playback
	if capture {
		ep = &e.capture
	}
	if e.closed {
		return "released"
	}
	if ep.problem() != "" {
		return "fault"
	}
	if !ep.enabled {
		return "released"
	}
	if e.onDemand && !ep.idleSince.IsZero() {
		return "idle"
	}
	return "open"
}
