package audio

import (
	"github.com/gen2brain/malgo"
	"testing"
)

func TestCoreAudioIDUsesUIDAndRejectsDefault(t *testing.T) {
	var a, b malgo.DeviceID
	if sameCoreAudioID(a, b) {
		t.Fatal("empty default IDs were accepted")
	}
	copy(a[:], "BlackHole_UID_2ch")
	copy(b[:], "BlackHole_UID_2ch")
	b[len(b)-1] = 42
	if !sameCoreAudioID(a, b) {
		t.Fatal("union padding changed device identity")
	}
	copy(b[:], "BlackHole_UID_16ch")
	if sameCoreAudioID(a, b) {
		t.Fatal("distinct UIDs matched")
	}
}
