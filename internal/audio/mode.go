package audio

import "fmt"

type Mode string

const (
	ModeMic      Mode = "mic"
	ModePlayback Mode = "playback"
	ModeDuplex   Mode = "duplex"
)

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	switch mode {
	case ModeMic, ModePlayback, ModeDuplex:
		return mode, nil
	}
	return "", fmt.Errorf("mode must be mic, playback or duplex")
}
func (m Mode) Microphone() bool { return m == ModeMic || m == ModeDuplex }
func (m Mode) Playback() bool   { return m == ModePlayback || m == ModeDuplex }
func (m Mode) Label() string {
	switch m {
	case ModeMic:
		return "手机麦克风"
	case ModePlayback:
		return "手机播放"
	case ModeDuplex:
		return "双向音频"
	}
	return "未连接"
}
