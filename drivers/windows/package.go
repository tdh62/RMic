package windowsdriver

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const SHA256 = "b950e39f01af1d04ea623c8f6d8eb9b6ea5c477c637295fabf20631c85116bfb"
const ConfigName = "vbcable-installer.json"

// LocalArchive only accepts the original base VB-CABLE package. No driver is embedded.
func LocalArchive(dir string) ([]byte, error) {
	config, err := os.ReadFile(filepath.Join(dir, ConfigName))
	if err != nil {
		return nil, fmt.Errorf("未启用本地 VB-CABLE 安装：%w", err)
	}
	var settings struct {
		Archive string `json:"archive"`
	}
	if err := json.Unmarshal(config, &settings); err != nil {
		return nil, fmt.Errorf("VB-CABLE 配置无效：%w", err)
	}
	if strings.TrimSpace(settings.Archive) == "" {
		return nil, fmt.Errorf("VB-CABLE 配置缺少 archive 路径")
	}
	path := settings.Archive
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20+1))
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != SHA256 {
		return nil, fmt.Errorf("VB-CABLE 原版 Pack45 ZIP 校验失败")
	}
	return data, nil
}

func Prepare(dir string) (string, error) {
	data, err := LocalArchive(dir)
	if err != nil {
		return "", err
	}
	return prepare(data, SHA256, "rmic-vbcable-", []string{"VBCABLE_Setup_x64.exe", "readme.txt"})
}

func prepare(data []byte, checksum, prefix string, required []string) (folder string, err error) {
	if fmt.Sprintf("%x", sha256.Sum256(data)) != checksum {
		return "", fmt.Errorf("driver package checksum mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	folder, err = os.MkdirTemp("", prefix)
	if err != nil {
		return "", err
	}
	created := folder
	defer func() {
		if err != nil {
			_ = os.RemoveAll(created)
		}
	}()
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if !filepath.IsLocal(name) || strings.Contains(name, ":") || file.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("invalid driver package path: %s", name)
		}
		target := filepath.Join(folder, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(target, 0700); err != nil {
				return "", err
			}
			continue
		}
		if file.UncompressedSize64 > 32<<20 {
			return "", fmt.Errorf("driver package member too large")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return "", err
		}
		input, openErr := file.Open()
		if openErr != nil {
			return "", openErr
		}
		output, createErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			_ = input.Close()
			return "", createErr
		}
		_, copyErr := io.Copy(output, input)
		inputErr := input.Close()
		outputErr := output.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if inputErr != nil {
			return "", inputErr
		}
		if outputErr != nil {
			return "", outputErr
		}
	}
	for _, name := range required {
		if _, err = os.Stat(filepath.Join(folder, name)); err != nil {
			return "", err
		}
	}
	return folder, nil
}
