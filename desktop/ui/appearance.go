package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

var appearanceNames = []string{"跟随系统", "浅色", "深色"}

func loadAppearance(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "gui-theme.json"))
	if os.IsNotExist(err) {
		return appearanceNames[0], nil
	}
	if err != nil {
		return appearanceNames[0], err
	}
	var value string
	if err = json.Unmarshal(data, &value); err != nil {
		return appearanceNames[0], err
	}
	if !slices.Contains(appearanceNames, value) {
		return appearanceNames[0], fmt.Errorf("未知主题：%s", value)
	}
	return value, nil
}
func saveAppearance(dir, value string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".theme-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, "gui-theme.json"))
}
