package transport

import (
	"fmt"
	"net/http"
	"strconv"

	"cnb.cool/tdh6/RMic/internal/audio"
)

// WithBootstrapToken keeps a process-local connection credential across an
// explicit listener restart. It must never be written to preferences.
func WithBootstrapToken(token string) Option { return func(s *Server) { s.token = token } }
func WithDefaultMode(mode audio.Mode) Option { return func(s *Server) { s.defaultMode = mode } }

func apiError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("X-RMic-Error", code)
	http.Error(w, message, status)
}
func (s *Server) checkRevision(w http.ResponseWriter, r *http.Request) bool {
	value := r.Header.Get("X-RMic-Config")
	if value == "" {
		return true
	} // Compatibility with previously loaded pages.
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil || revision != s.configRevision || r.Header.Get("X-RMic-Instance") != s.instanceID {
		apiError(w, "configuration_changed", "电脑配置已变化，请刷新能力后重新连接", 409)
		return false
	}
	return true
}

// Configure serializes configuration application with both negotiation paths.
// The caller owns the devices and may close detached devices after this returns.
// Defaults alone leave the active session and its revision unchanged.
func (s *Server) Configure(output Output, outputName string, input Input, inputName string, mode audio.Mode, qualityID string, replaceAudio bool) error {
	quality, err := audio.FindQuality(qualityID)
	if err != nil {
		return err
	}
	if err := quality.ValidateMode(mode); err != nil {
		return err
	}
	if _, err := audio.ParseMode(string(mode)); err != nil {
		return err
	}
	if output == nil && input == nil || mode.Microphone() && output == nil || mode.Playback() && input == nil {
		return fmt.Errorf("默认会话模式需要的音频设备未配置")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("服务已停止")
	}
	var previous *session
	if replaceAudio {
		s.StopShortcuts()
		previous = s.active
		s.active = nil
		if s.output != nil {
			s.output.SetSource(nil)
		}
		if s.input != nil {
			s.input.SetCaptureSink(nil)
		}
		if previous != nil {
			previous.cancel()
		}
		s.output, s.outputName, s.input, s.inputName = output, outputName, input, inputName
		s.configRevision++
		s.lastError = "电脑配置已更新"
	}
	s.defaultMode, s.defaultQuality = mode, qualityID
	s.mu.Unlock()
	if previous != nil {
		previous.closeConnections()
	}
	return nil
}
