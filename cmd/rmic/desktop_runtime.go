package main

import (
	"cnb.cool/tdh6/RMic/internal/app"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/tui"
	"cnb.cool/tdh6/RMic/internal/virtual"
	tea "github.com/charmbracelet/bubbletea"
)

// liveAudio owns the currently published engine. The route monitor and config
// application serialize access so detached devices can be closed safely.
type liveAudio struct {
	mu     sync.Mutex
	engine *audio.Engine
	mode   audio.Mode
}

func (a *liveAudio) Close() { a.mu.Lock(); defer a.mu.Unlock(); a.engine.Close() }
func checkAudioRoutes(engine *audio.Engine, mode audio.Mode) error {
	return app.CheckAudioRoutes(engine, mode)
}
func (a *liveAudio) watchRoutes(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			err := checkAudioRoutes(a.engine, a.mode)
			if err != nil && !errors.Is(err, virtual.ErrEndpointMissing) && !errors.Is(err, audio.ErrWindowsEndpointMissing) && !errors.Is(err, audio.ErrMacEndpointMissing) {
				a.engine.PauseCapture(err.Error() + "；电脑到手机方向已暂停，请检查路由并重新应用配置")
			}
			a.mu.Unlock()
		}
	}
}
func prepareAudio(settings launcher.Settings) (*audio.Engine, error) {
	return app.PrepareAudio(settings)
}
func audioEndpoints(engine *audio.Engine, mode audio.Mode) (transport.Output, transport.Input) {
	var output transport.Output
	var input transport.Input
	if mode.Microphone() {
		output = engine
	}
	if mode.Playback() {
		input = engine
	}
	return output, input
}
func copyAudioLevels(from, to *audio.Engine) {
	to.SetGain(from.Gain())
	to.SetCaptureGain(from.CaptureGain())
	if from.Muted() {
		to.ToggleMute()
	}
	if from.CaptureMuted() {
		to.ToggleCaptureMute()
	}
}
func (a *liveAudio) apply(server *transport.Server, dir string, current, next launcher.Settings) error {
	return a.applyStored(server, dir, current, next, next, current)
}
func (a *liveAudio) applyStored(server *transport.Server, dir string, current, next, stored, previousStored launcher.Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := next.Validate(); err != nil {
		return err
	}
	replace := current.AudioChanged(next) || a.engine.OutputError() != "" || a.engine.CaptureError() != ""
	candidate := a.engine
	if replace {
		var err error
		candidate, err = prepareAudio(next)
		if err != nil {
			return err
		}
		copyAudioLevels(a.engine, candidate)
	}
	// Save only after preparing and checking the replacement. A failed save must
	// leave both the running engine and session intact.
	if err := launcher.Save(dir, stored); err != nil {
		if replace {
			candidate.Close()
		}
		return err
	}
	output, input := audioEndpoints(candidate, next.Mode)
	if err := server.Configure(output, candidate.Name(), input, candidate.CaptureName(), next.SessionMode(), next.Quality, replace); err != nil {
		if replace {
			candidate.Close()
		}
		if restoreErr := launcher.Save(dir, previousStored); restoreErr != nil {
			return fmt.Errorf("%w；恢复配置文件失败：%v", err, restoreErr)
		}
		return err
	}
	if err := server.ConfigureShortcuts(next.Shortcuts); err != nil {
		return err
	}
	if replace {
		previous := a.engine
		a.engine, a.mode = candidate, next.Mode
		previous.Close()
	}
	return nil
}

func runLiveTUI(ctx context.Context, server *transport.Server, live *liveAudio, urls []string, inputLabel, dir string, manager *virtual.Manager, session *desktopSession) error {
	for {
		options := []tui.Option{tui.WithVirtualManager(manager)}
		if session != nil {
			options = append(options, tui.WithConfiguration(), tui.WithMaintenance(), tui.WithNotice(session.notice))
		}
		result, err := tui.Run(tui.New(server, live.engine, urls, inputLabel, options...), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
		if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if requested, ok := result.(interface{ MaintenanceRequested() bool }); ok && requested.MaintenanceRequested() {
			return errDriverMaintenance
		}
		requested, ok := result.(interface{ ConfigurationRequested() bool })
		if !ok || !requested.ConfigurationRequested() {
			return nil
		}
		restart := false
		var editMu sync.Mutex
		editingOpen := true
		if session.draft == nil {
			draft := session.stored
			session.draft = &draft
		}
		runningSettings := session.settings
		optionsSetup := tui.SetupOptions{
			Running: true, Directory: dir, Settings: session.stored, Draft: session.draft, RunningSettings: &runningSettings,
			Notice:   "编辑草稿期间保持传音；应用前请查看配置影响。",
			Discover: discoverDesktop, Manager: manager,
			Save: func(value launcher.Settings) error {
				editMu.Lock()
				defer editMu.Unlock()
				if !editingOpen || ctx.Err() != nil {
					return context.Canceled
				}
				if err := launcher.Save(dir, value); err != nil {
					return err
				}
				session.stored = value
				return nil
			},
			ApplyScope: func(stored launcher.Settings, scope string) error {
				editMu.Lock()
				defer editMu.Unlock()
				if !editingOpen || ctx.Err() != nil {
					return context.Canceled
				}
				next := tui.MergeScope(session.settings, stored, scope)
				if session.settings.NetworkChanged(next) {
					// Validate audio and routes before stopping the old listeners. If binding
					// the new listeners fails, runDesktop restores the previous settings.
					candidate, err := prepareAudio(next)
					if err != nil {
						return err
					}
					candidate.Close()
					if err := launcher.Save(dir, stored); err != nil {
						return err
					}
					session.previous = session.settings
					session.previousStored = session.stored
					session.settings = next
					session.stored = stored
					restart = true
					return nil
				}
				if err := live.applyStored(server, dir, session.settings, next, stored, session.stored); err != nil {
					return err
				}
				session.settings = next
				session.stored = stored
				return nil
			},
		}
		edited, err := tui.Run(tui.NewSetup(optionsSetup), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
		editMu.Lock()
		editingOpen = false
		editMu.Unlock()
		if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if edited.(*tui.Setup).MaintenanceRequested() {
			return errDriverMaintenance
		}
		if edited.(*tui.Setup).Start && restart {
			return errRestartService
		}
		if runtime.GOOS == "windows" {
			inputLabel = live.engine.MicrophonePeer()
		}
	}
}
