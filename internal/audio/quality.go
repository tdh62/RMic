package audio

import "fmt"

type Quality struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Bitrate      int    `json:"bitrate"`
	Channels     int    `json:"channels"`
	FrameMS      int    `json:"frame_ms"`
	BufferMS     int    `json:"buffer_ms"`
	PlaybackOnly bool   `json:"playback_only,omitempty"`
}

func Qualities() []Quality {
	return []Quality{
		{"voice", "清晰语音", 48000, 1, 20, 40, false},
		{"balanced", "均衡", 96000, 2, 20, 40, false},
		{"music", "高音质", 160000, 2, 20, 60, false},
		{"weak", "弱网络", 24000, 1, 40, 120, false},
		{"ultra", "Ultra 音乐播放", 384000, 2, 20, 120, true},
	}
}
func FindQuality(id string) (Quality, error) {
	for _, quality := range Qualities() {
		if quality.ID == id {
			return quality, nil
		}
	}
	return Quality{}, fmt.Errorf("quality must be voice, balanced, music, weak or ultra")
}
func (q Quality) ValidateMode(mode Mode) error {
	if q.PlaybackOnly && mode != ModePlayback {
		return fmt.Errorf("ultra 仅支持电脑 → 手机播放，请选择 playback 模式或其他音质")
	}
	return nil
}
func (q Quality) NewBuffer() *Buffer { return NewBufferWithLatency(q.BufferMS, max(200, q.BufferMS*3)) }
