package windowsdriver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalConfigurationGate(t *testing.T) {
	dir := t.TempDir()
	if _, err := Prepare(dir); err == nil {
		t.Fatal("missing config enabled installation")
	}
	for _, config := range []string{`{`, `{}`, `null`, `{"archive":"missing.zip"}`, `{"archive":"bad.zip"}`} {
		if err := os.WriteFile(filepath.Join(dir, ConfigName), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bad.zip"), []byte("not a vendor archive"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Prepare(dir); err == nil {
			t.Fatalf("accepted %s", config)
		}
	}
}

func TestConfiguredOriginalPackage(t *testing.T) {
	original, err := os.ReadFile("VBCABLE_Driver_Pack45.zip")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "original.zip"), original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"original.zip", filepath.Join(dir, "original.zip")} {
		config, _ := json.Marshal(map[string]string{"archive": path})
		if err := os.WriteFile(filepath.Join(dir, ConfigName), config, 0600); err != nil {
			t.Fatal(err)
		}
		folder, err := Prepare(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(folder)
		reader, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			input, err := reader.Open(entry.Name)
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			_, err = expected.ReadFrom(input)
			input.Close()
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(folder, entry.Name))
			if err != nil || !bytes.Equal(actual, expected.Bytes()) {
				t.Fatalf("vendor file changed: %s", entry.Name)
			}
		}
		readme, err := os.ReadFile(filepath.Join(folder, "readme.txt"))
		if err != nil || !strings.Contains(string(readme), "LICENSE AGREEMENT") {
			t.Fatal("original license missing")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "original.zip"), []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(dir); err == nil {
		t.Fatal("changed archive accepted")
	}
	if err := os.Remove(filepath.Join(dir, ConfigName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(dir); err == nil {
		t.Fatal("removed config accepted")
	}
}
