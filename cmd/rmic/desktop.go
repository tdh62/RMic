package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/tui"
	"cnb.cool/tdh6/RMic/internal/virtual"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func startupArgs(platform string, args []string) []string {
	if (platform == "windows" || platform == "darwin") && len(args) == 0 {
		return []string{"--setup"}
	}
	return args
}
func discoverDesktop() (tui.SetupDevices, error) {
	devices, err := app.Discover()
	return tui.SetupDevices{Windows: devices.Windows, MacOS: devices.MacOS, Outputs: devices.Outputs, Captures: devices.Captures, Networks: devices.Networks, Warning: devices.Warning}, err
}

var errRestartService = errors.New("apply network configuration")
var errDriverMaintenance = errors.New("stop service for driver maintenance")

type desktopSession struct {
	pairing        *pairing.Manager
	token          string
	settings       launcher.Settings
	previous       launcher.Settings
	draft          *launcher.Settings
	stored         launcher.Settings
	previousStored launcher.Settings
	maintenance    bool
	started        bool
	notice         string
}

func runDesktop(dir string, initialNotice ...string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("启动配置需要终端；命令行运行可用 --headless --mode mic --device 设备名称")
	}
	settings, loadErr := launcher.Load(dir)
	notice := strings.Join(initialNotice, "\n")
	if loadErr != nil {
		notice += "\n" + loadErr.Error() + "；已载入默认配置，保存前不会覆盖原文件。"
	}
	session := &desktopSession{}
	for {
		manager := virtual.NewManager(dir, true, true, nil)
		result, err := tui.Run(tui.NewSetup(tui.SetupOptions{
			Directory: dir, Settings: settings, Notice: notice, Discover: discoverDesktop, Manager: manager, OpenDrivers: session.maintenance, Draft: session.draft,
			Save: func(value launcher.Settings) error { return launcher.Save(dir, value) },
		}), tea.WithAltScreen(), tea.WithMouseCellMotion())
		closeErr := manager.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		setup := result.(*tui.Setup)
		if !setup.Start {
			return nil
		}
		settings = setup.Settings
		session.settings = settings
		session.stored = settings
		draft := settings
		session.draft = &draft
		session.maintenance = false
		var rollback *launcher.Settings
		for {
			err = runArgsWithSession(settings.Args(dir), true, session)
			if errors.Is(err, errDriverMaintenance) {
				session.maintenance = true
				settings = session.stored
				notice = "服务已停止，请选择要维护的驱动；维护后手动启动。"
				break
			}
			if errors.Is(err, errRestartService) {
				previous := session.previous
				rollback = &previous
				settings = session.settings
				continue
			}
			if err != nil && rollback != nil && !session.started {
				// Listener startup failed after a controlled restart. Restore the last
				// running configuration and keep process-local pairing credentials.
				notice = "网络配置应用失败：" + err.Error() + "；已恢复原配置。"
				fmt.Fprintln(os.Stderr, notice)
				session.notice = notice
				settings = *rollback
				session.settings = settings
				session.stored = session.previousStored
				if saveErr := launcher.Save(dir, session.stored); saveErr != nil {
					notice += " 保存恢复配置失败：" + saveErr.Error()
				}
				rollback = nil
				continue
			}
			break
		}
		if session.maintenance {
			continue
		}
		if err == nil {
			return nil
		}
		notice = "启动或运行失败：" + strings.TrimSpace(err.Error()) + "；可修改配置后重试。"
		settings = session.settings
	}
}
