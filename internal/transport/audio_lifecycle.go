package transport

// Called with s.mu held, only after an authorized audio connection is
// established. Visiting a page, pairing, requesting an offer or PCM ticket
// must not acquire system audio devices.
func (s *Server) activateAudioLocked(a *session) error {
	if a.mode.Microphone() {
		if device, ok := s.output.(interface{ EnsureOutput() error }); ok {
			if err := device.EnsureOutput(); err != nil {
				return err
			}
		}
	}
	if a.mode.Playback() {
		if device, ok := s.input.(interface{ EnsureCapture() error }); ok {
			if err := device.EnsureCapture(); err != nil {
				return err
			}
		}
	}
	if a.mode.Microphone() {
		s.output.SetSource(a.buffer)
	}
	if a.mode.Playback() {
		s.input.SetCaptureSink(a.sendBuffer)
	}
	return nil
}

func (s *Server) detachAudioLocked() {
	if s.output != nil {
		s.output.SetSource(nil)
	}
	if s.input != nil {
		s.input.SetCaptureSink(nil)
	}
}

func (d Direction) DeviceStateLabel() string {
	switch d.DeviceState {
	case "released":
		return "等待会话 · 音频设备已释放"
	case "idle":
		return "无会话 · 等待释放设备"
	case "open":
		return "音频设备已打开"
	case "fault":
		return "音频设备故障"
	default:
		return ""
	}
}
