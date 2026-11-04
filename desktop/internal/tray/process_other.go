//go:build !windows

package tray

import "os/exec"

func configure(cmd *exec.Cmd) {}
