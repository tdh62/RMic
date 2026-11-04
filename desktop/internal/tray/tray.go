// Package tray isolates the native tray lifetime from the GUI toolkit. Fyne's
// tray API cannot remove an icon at runtime; a helper process can fully exit
// and be restarted without leaving a tray icon attached to the main window.
package tray

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"cnb.cool/tdh6/RMic/desktop/assets"
	"fyne.io/systray"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const HelperArg = "--internal-gui-tray"

type Process struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	done  chan struct{}
}

// Start reports events on a background goroutine, ending with "stopped".
// The caller must marshal callbacks onto its UI thread.
func Start(event func(string)) (*Process, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, HelperArg)
	configure(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	p := &Process{cmd: cmd, input: input, done: make(chan struct{})}
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			event(scanner.Text())
		}
		_ = cmd.Wait()
		close(p.done)
		event("stopped")
	}()
	return p, nil
}

// Stop removes the icon through native cleanup; kill is a bounded fallback.
func (p *Process) Stop() {
	_ = p.input.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func Run() error {
	runtime.LockOSThread()
	if err := available(); err != nil {
		return err
	}
	icon, err := iconBytes()
	if err != nil {
		return err
	}
	systray.Run(func() {
		systray.SetIcon(icon)
		systray.SetTitle("RMic")
		systray.SetTooltip("RMic GUI · 双击或菜单恢复窗口")
		show := systray.AddMenuItem("显示 RMic", "恢复主窗口")
		disable := systray.AddMenuItem("关闭托盘并显示窗口", "移除托盘图标")
		systray.AddSeparator()
		quit := systray.AddMenuItem("退出 RMic", "停止传音并退出")
		systray.SetOnTapped(func() { fmt.Println("show") })
		fmt.Println("ready")
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); systray.Quit() }()
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if available() != nil {
					systray.Quit()
					return
				}
			}
		}()
		go func() {
			for {
				select {
				case <-show.ClickedCh:
					fmt.Println("show")
				case <-disable.ClickedCh:
					fmt.Println("disable")
				case <-quit.ClickedCh:
					fmt.Println("quit")
				}
			}
		}()
	}, nil)
	return nil
}

func iconBytes() ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(assets.Icon))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, 64, 64)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	icon.Draw(rasterx.NewDasher(64, 64, rasterx.NewScannerGV(64, 64, img, img.Bounds())), 1)
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		return data.Bytes(), nil
	}
	// ICO directory with a single PNG image, supported by Windows Vista+.
	header := make([]byte, 22)
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], 1)
	header[6], header[7] = 64, 64
	binary.LittleEndian.PutUint16(header[10:], 1)
	binary.LittleEndian.PutUint16(header[12:], 32)
	binary.LittleEndian.PutUint32(header[14:], uint32(data.Len()))
	binary.LittleEndian.PutUint32(header[18:], 22)
	return append(header, data.Bytes()...), nil
}
