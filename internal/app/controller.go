package app

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
)

// Controller owns a local desktop lifetime. No frontend objects or callbacks
// are used here. Commands serialize with monitoring and Close.
type Controller struct {
	mu          sync.Mutex
	dir         string
	unlock      func()
	pairing     *pairing.Manager
	manager     *virtual.Manager
	run         *service
	settings    launcher.Settings
	closed      bool
	lastError   string
	done        chan struct{}
	monitorDone chan struct{}
}

type service struct {
	engine    *audio.Engine
	server    *transport.Server
	http      *http.Server
	listeners []net.Listener
	urls      []string
	serveErr  chan error
}

type Snapshot struct {
	Running  bool
	Settings launcher.Settings
	Status   transport.Status
	URLs     []string
	Requests []pairing.Request
	Devices  []pairing.Device
	Error    string
}

func Open(dir string) (*Controller, error) {
	unlock, err := launcher.LockDirectory(dir)
	if err != nil {
		return nil, err
	}
	p, err := pairing.Open(dir)
	if err != nil {
		unlock()
		return nil, err
	}
	c := &Controller{dir: dir, unlock: unlock, pairing: p, manager: virtual.NewManager(dir, true, true, nil), done: make(chan struct{}), monitorDone: make(chan struct{})}
	go c.monitor()
	return c, nil
}

func (c *Controller) available() error {
	if c.closed {
		return fmt.Errorf("应用已关闭")
	}
	return nil
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{Settings: c.settings, Error: c.lastError}
	if c.closed {
		return s
	}
	s.Requests, s.Devices = c.pairing.Requests(), c.pairing.Devices()
	if c.run != nil {
		s.Running = true
		s.Status = c.run.server.Status()
		s.URLs = append([]string(nil), c.run.urls...)
	}
	return s
}

func (c *Controller) start(settings launcher.Settings) (*service, error) {
	engine, err := PrepareAudio(settings)
	if err != nil {
		return nil, err
	}
	r := &service{engine: engine, serveErr: make(chan error, 1)}
	ok := false
	defer func() {
		if !ok {
			r.close()
		}
	}()
	addresses, err := transport.Interfaces()
	if err != nil {
		return nil, err
	}
	ips, err := transport.SelectAddresses(addresses, "", settings.Network)
	if err != nil {
		return nil, err
	}
	var output transport.Output
	options := []transport.Option{transport.WithNetwork(ips), transport.WithQuality(settings.Quality), transport.WithDefaultMode(settings.SessionMode()), transport.WithPairing(c.pairing), transport.WithShortcuts(settings.Shortcuts)}
	if settings.Mode.Microphone() {
		output = engine
	}
	if settings.Mode.Playback() {
		options = append(options, transport.WithInput(engine, engine.CaptureName()))
	}
	r.server, err = transport.NewServer(output, engine.Name(), settings.UDPPort, options...)
	if err != nil {
		return nil, err
	}
	r.listeners, err = transport.ListenTCP(":"+strconv.Itoa(settings.HTTPSPort), ips)
	if err != nil {
		return nil, err
	}
	certificate, err := transport.Certificate(c.dir, ips)
	if err != nil {
		return nil, err
	}
	r.http = &http.Server{Handler: r.server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, ErrorLog: log.New(io.Discard, "", 0)}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			r.urls = append(r.urls, "https://"+net.JoinHostPort(ip.String(), strconv.Itoa(settings.HTTPSPort))+"/")
		}
	}
	if len(r.urls) == 0 {
		for _, ip := range ips {
			r.urls = append(r.urls, "https://"+net.JoinHostPort(ip.String(), strconv.Itoa(settings.HTTPSPort))+"/")
		}
	}
	for _, listener := range r.listeners {
		go func(l net.Listener) {
			if err := r.http.ServeTLS(l, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				select {
				case r.serveErr <- err:
				default:
				}
			}
		}(listener)
	}
	ok = true
	return r, nil
}

func (r *service) close() {
	if r.http != nil {
		_ = r.http.Close()
	}
	for _, l := range r.listeners {
		_ = l.Close()
	}
	if r.server != nil {
		r.server.Close()
	}
	if r.engine != nil {
		r.engine.Close()
	}
}

func (c *Controller) Start(settings launcher.Settings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.run != nil {
		return fmt.Errorf("服务已在运行，请使用应用配置")
	}
	r, err := c.start(settings)
	if err != nil {
		return err
	}
	if err = launcher.Save(c.dir, settings); err != nil {
		r.close()
		return err
	}
	c.run, c.settings, c.lastError = r, settings, ""
	return nil
}

func (c *Controller) Save(settings launcher.Settings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	return launcher.Save(c.dir, settings)
}

func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.run != nil {
		c.run.close()
		c.run = nil
	}
	return nil
}

func copyLevels(from, to *audio.Engine) {
	to.SetGain(from.Gain())
	to.SetCaptureGain(from.CaptureGain())
	if from.Muted() {
		to.ToggleMute()
	}
	if from.CaptureMuted() {
		to.ToggleCaptureMute()
	}
}

// Apply keeps the current engine until its replacement has been prepared.
// Network changes require releasing the listeners, with explicit rollback.
func (c *Controller) Apply(next launcher.Settings) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if c.run == nil {
		return fmt.Errorf("服务未运行，请保存并启动")
	}
	current := c.settings
	if current.NetworkChanged(next) {
		candidate, err := PrepareAudio(next)
		if err != nil {
			return err
		}
		candidate.Close()
		gain, captureGain := c.run.engine.Gain(), c.run.engine.CaptureGain()
		muted, captureMuted := c.run.engine.Muted(), c.run.engine.CaptureMuted()
		c.run.close()
		c.run = nil
		r, startErr := c.start(next)
		if startErr == nil {
			if startErr = launcher.Save(c.dir, next); startErr != nil {
				r.close()
			}
		}
		if startErr != nil {
			restored, restoreErr := c.start(current)
			if restoreErr != nil {
				c.lastError = fmt.Sprintf("应用失败：%v；恢复失败：%v；服务已停止", startErr, restoreErr)
				return errors.New(c.lastError)
			}
			c.run = restored
		} else {
			c.run, c.settings = r, next
		}
		c.run.engine.SetGain(gain)
		c.run.engine.SetCaptureGain(captureGain)
		if muted {
			c.run.engine.ToggleMute()
		}
		if captureMuted {
			c.run.engine.ToggleCaptureMute()
		}
		if startErr != nil {
			return fmt.Errorf("应用失败：%w；已恢复原运行配置", startErr)
		}
		c.lastError = ""
		return nil
	}
	replace := current.AudioChanged(next) || c.run.engine.OutputError() != "" || c.run.engine.CaptureError() != ""
	candidate := c.run.engine
	if replace {
		var err error
		candidate, err = PrepareAudio(next)
		if err != nil {
			return err
		}
		copyLevels(c.run.engine, candidate)
	}
	if err := launcher.Save(c.dir, next); err != nil {
		if replace {
			candidate.Close()
		}
		return err
	}
	var output transport.Output
	var input transport.Input
	if next.Mode.Microphone() {
		output = candidate
	}
	if next.Mode.Playback() {
		input = candidate
	}
	// ConfigureShortcuts validates before changing its runtime. Settings.Validate
	// has already checked platform support and every step.
	if err := c.run.server.Configure(output, candidate.Name(), input, candidate.CaptureName(), next.SessionMode(), next.Quality, replace); err != nil {
		if replace {
			candidate.Close()
		}
		return errors.Join(err, launcher.Save(c.dir, current))
	}
	if replace {
		old := c.run.engine
		c.run.engine = candidate
		old.Close()
	}
	c.settings = next
	if err := c.run.server.ConfigureShortcuts(next.Shortcuts); err != nil {
		return fmt.Errorf("音频配置已应用，快捷按钮配置失败：%w", err)
	}
	c.lastError = ""
	return nil
}

func (c *Controller) SetLevels(send bool, gain float64, muted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.run == nil {
		return fmt.Errorf("服务未运行")
	}
	e := c.run.engine
	if send {
		e.SetCaptureGain(gain)
		if e.CaptureMuted() != muted {
			e.ToggleCaptureMute()
		}
	} else {
		e.SetGain(gain)
		if e.Muted() != muted {
			e.ToggleMute()
		}
	}
	return nil
}

func (c *Controller) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.run != nil {
		c.run.server.Disconnect()
	}
	return nil
}

func (c *Controller) Decide(id string, allow, once bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if allow && once {
		return c.pairing.DecideOnce(id)
	}
	return c.pairing.Decide(id, allow)
}

func (c *Controller) Revoke(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	return c.pairing.Revoke(id)
}

// InspectDrivers is read-only and can run while an audio session is active.
func (c *Controller) InspectDrivers() []virtual.DriverInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return []virtual.DriverInfo{{Error: err.Error()}}
	}
	return c.manager.Inspect()
}

func (c *Controller) Driver(action string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return "", err
	}
	if c.run != nil {
		return "", fmt.Errorf("请先停止服务，再管理虚拟设备")
	}
	return c.manager.Run(action)
}

func (c *Controller) StopShortcuts() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.run != nil {
		c.run.server.StopShortcuts()
	}
	return nil
}

func (c *Controller) monitor() {
	defer close(c.monitorDone)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			if !c.closed && c.run != nil {
				select {
				case err := <-c.run.serveErr:
					c.lastError = "HTTPS 服务停止：" + err.Error()
					c.run.close()
					c.run = nil
				default:
					if err := CheckAudioRoutes(c.run.engine, c.settings.Mode); err != nil && !errors.Is(err, virtual.ErrEndpointMissing) && !errors.Is(err, audio.ErrWindowsEndpointMissing) && !errors.Is(err, audio.ErrMacEndpointMissing) {
						c.run.engine.PauseCapture(err.Error() + "；请检查路由并重新应用配置")
					}
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	if c.run != nil {
		c.run.close()
		c.run = nil
	}
	err := c.manager.Close()
	c.unlock()
	c.mu.Unlock()
	<-c.monitorDone
	return err
}
