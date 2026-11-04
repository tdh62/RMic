package ui

import (
	"os"
	"testing"

	"golang.org/x/image/font/sfnt"
)

func TestInstalledCJKCollection(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc")
	if os.IsNotExist(err) {
		t.Skip("system Noto CJK collection not installed")
	}
	if err != nil {
		t.Fatal(err)
	}
	face, err := standaloneFont(data)
	if err != nil {
		t.Fatal(err)
	}
	font, err := sfnt.Parse(face)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	for _, r := range "手机麦克风配对" {
		glyph, err := font.GlyphIndex(&buffer, r)
		if err != nil || glyph == 0 {
			t.Fatalf("missing Chinese glyph %c: %v", r, err)
		}
	}
}

func TestMalformedFontRejected(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("ttcf"), append([]byte("ttcf"), make([]byte, 12)...), make([]byte, 20)} {
		if _, err := standaloneFont(data); err == nil {
			t.Fatal("invalid font accepted")
		}
	}
}
