package shortcuts

import "sort"

// Keep modifiers before ordinary keys and use a stable order in recorded chords.
var captureKeyOrder = []string{"ControlLeft", "ControlRight", "ShiftLeft", "ShiftRight", "AltLeft", "AltRight", "MetaLeft", "MetaRight"}

// Build the ordinary key list after all package initializers populate keyCodes.
func captureKeys() []string {
	keys := append([]string(nil), captureKeyOrder...)
	var ordinary []string
	for name := range keyCodes {
		modifier := false
		for _, key := range keys[:8] {
			if name == key {
				modifier = true
				break
			}
		}
		if !modifier {
			ordinary = append(ordinary, name)
		}
	}
	sort.Strings(ordinary)
	return append(keys, ordinary...)
}
