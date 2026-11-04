//go:build !windows

package audio

func microphoneListening(string) (bool, error) { return false, nil }
