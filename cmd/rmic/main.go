package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/buildinfo"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/tui"
	"cnb.cool/tdh6/RMic/internal/virtual"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "RMic:", err)
		waitOnError()
		os.Exit(1)
	}
}
func run() error {
	prepareConsole()
	args := startupArgs(runtime.GOOS, os.Args[1:])
	return runArgs(args, false)
}

func runArgs(args []string, fromSetup bool) error {
	return runArgsWithSession(args, fromSetup, nil)
}

func runArgsWithSession(args []string, fromSetup bool, session *desktopSession) error {
	if session != nil {
		session.started = false
	}
	if len(args) == 1 && strings.HasPrefix(args[0], "--internal-steam-driver=") {
		fmt.Println("正在执行 Steam 虚拟音频驱动操作，请等待完成。")
		message, err := virtual.ApplySteamDriver(strings.TrimPrefix(args[0], "--internal-steam-driver="))
		if message != "" {
			fmt.Println(message)
		}
		return err
	}
	flags := flag.NewFlagSet("rmic", flag.ContinueOnError)
	driver := flags.String("driver", "vbcable", "Windows driver: vbcable (manual by default; local installer config required), steam (status only)")
	setup := flags.Bool("setup", false, "open startup TUI to configure audio, network and driver settings")
	version := flags.Bool("version", false, "print version and build information, then exit")
	listen := flags.String("listen", ":8443", "HTTPS listen address")
	advertise := flags.String("ip", "", "optional advertised IPv4/IPv6 address (for container/NAT mappings)")
	interfaces := flags.String("interfaces", "", "comma-separated interface names; explicitly selected interfaces may have public IPs")
	bindIPs := flags.String("bind-ips", "", "comma-separated local IPv4/IPv6 addresses for HTTPS and WebRTC")
	listInterfaces := flags.Bool("list-interfaces", false, "list all interface addresses and default selection, then exit")
	qualityID := flags.String("quality", "balanced", "default audio quality: voice, balanced, music, weak or ultra (playback only)")
	udp := flags.Int("udp-port", 50000, "WebRTC UDP port; allow this port through the firewall")
	device := flags.String("device", "", "phone microphone output device (virtual sound card playback endpoint)")
	idleTimeout := flags.Duration("audio-idle-timeout", 30*time.Second, "release unused audio devices after this delay (1s–1h)")
	captureChannels := flags.Int("capture-channels", 2, "computer capture channels: 2, 6 (5.1 side), or 8 (7.1)")
	capture := flags.String("capture", "", "computer audio capture device (independent virtual playback recording endpoint)")
	direction := flags.String("mode", "duplex", "permitted audio directions: mic, playback or duplex")
	defaultDirection := flags.String("default-mode", "", "initial phone session mode within the permitted directions")
	virtualMode := flags.String("virtual", "auto", "virtual devices: auto, off, install, uninstall or status")
	virtualManagerOnly := flags.Bool("virtual-manager", false, "open virtual audio installation TUI without starting audio or network services")
	list := flags.Bool("list-devices", false, "list native playback and capture devices and exit")
	headless := flags.Bool("headless", false, "run without interactive TUI")
	portable := flags.Bool("portable", launcher.DefaultPortable(), "store settings and authorization in rmic-data beside the executable")
	config := flags.String("config-dir", "", "explicit directory for settings, TLS, authorization and virtual-device records")
	certFile := flags.String("cert", "", "custom trusted TLS certificate PEM (requires --key)")
	keyFile := flags.String("key", "", "custom TLS private key PEM")
	virtualCommand := ""
	if len(args) > 0 && args[0] == "virtual" {
		if len(args) < 2 {
			return fmt.Errorf("usage: rmic virtual install|status|uninstall [--mode mic|playback|duplex]")
		}
		virtualCommand = args[1]
		args = args[2:]
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *driver != "vbcable" && *driver != "steam" && *driver != "vbcable-a" && *driver != "vbcable-b" {
		return fmt.Errorf("--driver must be steam, vbcable, vbcable-a or vbcable-b")
	}
	if *version {
		fmt.Printf("RMic %s\ncommit: %s\nbuilt: %s\nplatform: %s/%s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, runtime.GOOS, runtime.GOARCH)
		return nil
	}
	notice := ""
	if !*list && !*listInterfaces || *setup || *virtualManagerOnly {
		directory, warning, err := launcher.ConfigDirectory(*config, *portable)
		if err != nil {
			return err
		}
		*config = directory
		notice = warning
		if warning != "" {
			fmt.Fprintln(os.Stderr, warning)
		}
	}
	if !fromSetup && (!*list && !*listInterfaces || *setup || *virtualManagerOnly) {
		unlock, err := launcher.LockDirectory(*config)
		if err != nil {
			return err
		}
		defer unlock()
	}
	if *setup {
		return runDesktop(*config, notice)
	}
	mode, err := audio.ParseMode(*direction)
	if err != nil {
		return err
	}
	if *virtualManagerOnly {
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("--virtual-manager requires a terminal; use 'rmic virtual install|status|uninstall' in scripts")
		}
		manager := virtual.NewManager(*config, mode.Microphone(), mode.Playback(), nil)
		defer manager.Close()
		_, err := tui.Run(tui.NewVirtual(manager, tui.WithWindowsDriver(*driver)), tea.WithAltScreen(), tea.WithMouseCellMotion())
		return err
	}
	if *idleTimeout < time.Second || *idleTimeout > time.Hour {
		return fmt.Errorf("--audio-idle-timeout must be between 1s and 1h")
	}
	if err := audio.ValidateChannels(*captureChannels); err != nil {
		return err
	}
	defaultMode := mode
	if *defaultDirection != "" {
		defaultMode, err = audio.ParseMode(*defaultDirection)
		if err != nil || !launcher.Allows(mode, defaultMode) {
			return fmt.Errorf("--default-mode must be within --mode directions")
		}
	} else if *qualityID == "ultra" && mode.Playback() {
		defaultMode = audio.ModePlayback
	} else if mode == audio.ModeDuplex {
		defaultMode = audio.ModeMic
	}
	quality, err := audio.FindQuality(*qualityID)
	if err != nil {
		return err
	}
	if err = quality.ValidateMode(defaultMode); err != nil {
		return err
	}
	if *listInterfaces {
		addresses, err := transport.Interfaces()
		if err != nil {
			return err
		}
		for _, address := range addresses {
			fmt.Printf("%-20s %-40s up=%t default=%t\n", address.Interface, address.IP, address.Up, transport.DefaultAddress(address))
		}
		return nil
	}
	if virtualCommand != "" {
		*virtualMode = virtualCommand
	}
	if *virtualMode == "install" || *virtualMode == "uninstall" || *virtualMode == "status" {
		action := *virtualMode
		if *driver == "steam" {
			action = "steam-" + action
		} else if *driver == "vbcable-a" {
			action = "cable-a-" + action
		} else if *driver == "vbcable-b" {
			action = "cable-b-" + action
		}
		message, err := virtual.Manage(action, *config, mode.Microphone(), mode.Playback())
		if err == nil {
			fmt.Println(message)
		}
		return err
	}
	if *virtualMode != "auto" && *virtualMode != "off" {
		return fmt.Errorf("--virtual must be auto, off, install, uninstall or status")
	}
	if *udp < 0 || *udp > 65535 {
		return fmt.Errorf("invalid UDP port")
	}
	if (*certFile == "") != (*keyFile == "") {
		return fmt.Errorf("--cert and --key must be provided together")
	}
	if !*list && mode == audio.ModeDuplex && runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return fmt.Errorf("duplex routing validation is supported on Linux, Windows and macOS")
	}
	var virtualDevice *virtual.Device
	createMic := mode.Microphone() && *device == ""
	createPlayback := mode.Playback() && *capture == ""
	if !*list && *virtualMode == "auto" && runtime.GOOS == "linux" {
		installed, err := virtual.Installed(*config)
		if err != nil {
			return err
		}
		if installed != nil {
			if createMic && installed.Output != "" {
				*device = installed.Output
				createMic = false
			}
			if createPlayback && installed.Capture != "" {
				*capture = installed.Capture
				createPlayback = false
			}
		}
	}
	if !*list && *virtualMode == "auto" && (createMic || createPlayback) && runtime.GOOS == "linux" {
		virtualDevice, err = virtual.CreateFor(createMic, createPlayback)
		if err != nil {
			return fmt.Errorf("create virtual audio devices: %w; ensure your desktop PulseAudio/PipeWire session is available", err)
		}
		if createMic {
			*device = virtualDevice.Output
		}
		if createPlayback {
			*capture = virtualDevice.Capture
		}
	}
	manager := virtual.NewManager(*config, mode.Microphone(), mode.Playback(), virtualDevice)
	defer func() {
		if err := manager.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	engine, err := audio.NewEngine()
	if err != nil {
		return err
	}
	live := &liveAudio{engine: engine, mode: mode}
	defer live.Close()
	if *list {
		fmt.Println("Playback devices (--device):")
		for _, name := range engine.MicrophoneDevices() {
			fmt.Println("  " + name)
		}
		fmt.Println("Capture devices (--capture):")
		for _, name := range engine.CaptureDevices() {
			fmt.Println("  " + name)
		}
		return nil
	}
	var output transport.Output
	var options []transport.Option
	outputMatch, captureMatch := "", ""
	if mode.Microphone() {
		outputMatch = *device
		if outputMatch == "" {
			return fmt.Errorf("请选择麦克风送入设备")
		}
	}
	if mode.Playback() {
		captureMatch = *capture
		if captureMatch == "" {
			return fmt.Errorf("请选择电脑声音来源")
		}
	}
	if err = engine.Prepare(outputMatch, captureMatch, *captureChannels, *idleTimeout); err != nil {
		return err
	}
	if mode.Microphone() {
		output = engine
	}
	if mode.Playback() {
		options = append(options, transport.WithInput(engine, engine.CaptureName()))
	}
	if err = checkAudioRoutes(engine, mode); err != nil {
		return err
	}
	addresses, err := transport.Interfaces()
	if err != nil {
		return err
	}
	ips, err := transport.SelectAddresses(addresses, *interfaces, *bindIPs)
	if err != nil {
		return err
	}
	options = append(options, transport.WithNetwork(ips), transport.WithQuality(*qualityID), transport.WithDefaultMode(defaultMode))
	advertised := []net.IP{}
	certIPs := append([]net.IP(nil), ips...)
	if *advertise != "" {
		ip := net.ParseIP(*advertise)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("--ip must be a reachable IPv4 or IPv6 unicast address")
		}
		advertised = append(advertised, ip)
		certIPs = append(certIPs, ip)
	} else {
		for _, ip := range ips {
			if !ip.IsLoopback() {
				advertised = append(advertised, ip)
			}
		}
		if len(advertised) == 0 {
			advertised = append(advertised, ips...)
		}
	}
	var pairingManager *pairing.Manager
	if session != nil {
		pairingManager = session.pairing
	}
	if pairingManager == nil {
		pairingManager, err = pairing.Open(*config)
		if err != nil {
			return err
		}
		if session != nil {
			session.pairing = pairingManager
		}
	}
	options = append(options, transport.WithPairing(pairingManager))
	if session != nil && session.token != "" {
		options = append(options, transport.WithBootstrapToken(session.token))
	}
	shortcutSettings := launcher.Default()
	if session != nil {
		shortcutSettings = session.settings
	} else {
		shortcutSettings, err = launcher.Load(*config)
		if err != nil {
			return err
		}
	}
	options = append(options, transport.WithShortcuts(shortcutSettings.Shortcuts))
	server, err := transport.NewServer(output, engine.Name(), *udp, options...)
	if err != nil {
		return err
	}
	defer server.Close()
	if session != nil {
		session.token = server.Token()
	}
	listeners, err := transport.ListenTCP(*listen, ips)
	if err != nil {
		return fmt.Errorf("HTTPS listener: %w", err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	// Bind first so a rejected network update cannot replace the trusted TLS
	// identity before we know its listeners are available.
	var certificate tls.Certificate
	if *certFile != "" {
		certificate, err = tls.LoadX509KeyPair(*certFile, *keyFile)
	} else {
		certificate, err = transport.Certificate(*config, certIPs)
	}
	if err != nil {
		return fmt.Errorf("TLS certificate: %w", err)
	}
	if session != nil {
		session.started = true
	}
	_, port, _ := net.SplitHostPort(listeners[0].Addr().String())
	urls := []string{}
	for _, ip := range advertised {
		urls = append(urls, "https://"+net.JoinHostPort(ip.String(), port)+"/")
	}
	input := "在目标软件中选择虚拟声卡的录音端"
	if runtime.GOOS == "windows" {
		input = engine.MicrophonePeer()
	}
	if virtualDevice != nil && virtualDevice.Input != "" {
		input = virtualDevice.Input
	}
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, ErrorLog: log.New(io.Discard, "", 0)}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	routeCtx, stopRoutes := context.WithCancel(ctx)
	routesDone := make(chan struct{})
	go func() { defer close(routesDone); live.watchRoutes(routeCtx) }()
	defer func() { stopRoutes(); <-routesDone }()
	serveErr := make(chan error, 1)
	for _, listener := range listeners {
		go func(listener net.Listener) {
			err := httpServer.ServeTLS(listener, "", "")
			if !errors.Is(err, http.ErrServerClosed) {
				select {
				case serveErr <- err:
				default:
				}
				cancel()
			}
		}(listener)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
		}
	}()
	fmt.Printf("RMic: HTTPS %s · WebRTC UDP %s\n", port, strings.Join(server.Status().Network, ", "))
	if *headless || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		for _, url := range urls {
			fmt.Println("设备连接地址:", url+"#token="+server.Token())
		}
		fmt.Println("可用模式:", mode.Label(), "· 默认音质:", *qualityID)
		if mode.Microphone() {
			fmt.Println("目标软件麦克风:", input)
		}
		if mode.Playback() {
			fmt.Println("电脑音频采集:", engine.CaptureName())
			if virtualDevice != nil && virtualDevice.Playback != "" {
				fmt.Println("请手动将目标软件播放设备设为:", virtualDevice.Playback)
			}
		}
		fmt.Println("证书目录:", *config, "· 使用 Ctrl+C 退出")
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				select {
				case err := <-serveErr:
					return err
				default:
					return nil
				}
			case <-ticker.C:
				status := server.Status()
				fmt.Printf("state=%s mode=%s mic=%s gain=%s mute=%t playback=%s gain=%.1f mute=%t\n", status.State, status.Mode, status.Receive.State, strconv.FormatFloat(status.Gain, 'f', 1, 64), status.Muted, status.Send.State, status.Send.Gain, status.Send.Muted)
			}
		}
	}
	if !fromSetup {
		session = nil
	}
	err = runLiveTUI(ctx, server, live, urls, input, *config, manager, session)
	select {
	case serveFailure := <-serveErr:
		return serveFailure
	default:
		return err
	}
}
