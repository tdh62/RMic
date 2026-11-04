package audio

import "fmt"

// Layout follows WAVEFORMATEXTENSIBLE order. 5.1 uses side surrounds.
func Layout(channels int) []string {
	switch channels {
	case 2:
		return []string{"FL", "FR"}
	case 6:
		return []string{"FL", "FR", "FC", "LFE", "SL", "SR"}
	case 8:
		return []string{"FL", "FR", "FC", "LFE", "BL", "BR", "SL", "SR"}
	}
	return nil
}
func ValidateChannels(channels int) error {
	if Layout(channels) == nil {
		return fmt.Errorf("声道数必须为 2、6（5.1）或 8（7.1）")
	}
	return nil
}

// Stereo writes complete frames; weights include headroom and intentionally
// omit LFE. This is an explicit downmix, not a claim of surround reproduction.
func Stereo(pcm []int16, channels int) []int16 {
	if channels == 2 {
		return pcm
	}
	out := make([]int16, len(pcm)/channels*2)
	for frame := 0; frame < len(out)/2; frame++ {
		in := pcm[frame*channels:]
		left, right := float64(in[0]), float64(in[1])
		left += 0.7071 * float64(in[2])
		right += 0.7071 * float64(in[2])
		if channels == 6 {
			left += 0.7071 * float64(in[4])
			right += 0.7071 * float64(in[5])
		} else {
			left += 0.7071*float64(in[4]) + 0.7071*float64(in[6])
			right += 0.7071*float64(in[5]) + 0.7071*float64(in[7])
		}
		divisor := 2.4142
		if channels == 8 {
			divisor = 3.1213
		}
		out[frame*2] = int16(left / divisor)
		out[frame*2+1] = int16(right / divisor)
	}
	return out
}
