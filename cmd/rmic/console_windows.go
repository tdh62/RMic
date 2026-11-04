//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/term"
	"os"
	"strings"
	"unsafe"
)

func waitOnError() {
	if (len(os.Args) == 1 || len(os.Args) == 2 && strings.HasPrefix(os.Args[1], "--internal-steam-driver=")) && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "按 Enter 关闭窗口。")
		var line string
		_, _ = fmt.Fscanln(os.Stdin, &line)
	}
}

func prepareConsole() {
	// A console-subsystem executable opens a console automatically on double
	// click. Bubble Tea manages input mode; enable UTF-8 and ANSI output here.
	_ = windows.SetConsoleCP(65001)
	_ = windows.SetConsoleOutputCP(65001)
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err == nil {
		var mode uint32
		if windows.GetConsoleMode(handle, &mode) == nil {
			_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		}
	}
	title, _ := windows.UTF16PtrFromString("RMic · 音频与设备配置")
	windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(title)))
}
