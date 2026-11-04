package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"cnb.cool/tdh6/RMic/desktop/assets"
	"cnb.cool/tdh6/RMic/desktop/internal/tray"
	"cnb.cool/tdh6/RMic/desktop/ui"
	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/buildinfo"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == tray.HelperArg {
		if err := tray.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// The signed-driver helper must execute before initializing a GUI or taking
	// the desktop's directory lock. It is invoked through the existing UAC flow.
	if len(os.Args) == 2 && strings.HasPrefix(os.Args[1], "--internal-steam-driver=") {
		message, err := virtual.ApplySteamDriver(strings.TrimPrefix(os.Args[1], "--internal-steam-driver="))
		fmt.Println(message)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	flags := flag.NewFlagSet("rmic-gui", flag.ContinueOnError)
	directory := flags.String("config-dir", "", "explicit configuration directory")
	portable := flags.Bool("portable", launcher.DefaultPortable(), "use rmic-data beside the executable")
	version := flags.Bool("version", false, "print build information")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return
	}
	if *version {
		fmt.Printf("RMic GUI %s\ncommit: %s\nbuilt: %s\nplatform: %s/%s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, runtime.GOOS, runtime.GOARCH)
		return
	}
	a := app.NewWithID("cool.cnb.rmic.gui")
	a.SetIcon(fyne.NewStaticResource("rmic-gui.svg", assets.Icon))
	ui.SetTheme(a)
	w := a.NewWindow("RMic GUI")
	w.Resize(fyne.NewSize(820, 540))
	fail := func(err error) {
		label := widget.NewLabel("启动失败：" + err.Error())
		label.Wrapping = fyne.TextWrapWord
		w.SetContent(container.NewBorder(nil, widget.NewButton("退出", a.Quit), nil, nil, label))
		w.ShowAndRun()
	}
	if flags.NArg() != 0 {
		fail(fmt.Errorf("不支持的位置参数"))
		return
	}
	dir, notice, err := launcher.ConfigDirectory(*directory, *portable)
	if err != nil {
		fail(err)
		return
	}
	controller, err := core.Open(dir)
	if err != nil {
		fail(err)
		return
	}
	defer controller.Close()
	settings, loadErr := launcher.Load(dir)
	if loadErr != nil {
		notice += "\n" + loadErr.Error() + "；已显示默认值，保存前不会覆盖文件。"
	}
	v := ui.New(w, controller, dir, settings, notice)
	a.Lifecycle().SetOnEnteredForeground(v.Resume)
	defer v.CloseTray()
	w.ShowAndRun()
}
