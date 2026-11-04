// Package virtual manages RMic virtual audio modules using ownership tags.
package virtual

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type ownedModule struct{ ID, Name, Argument string }
type Device struct {
	Output, Input     string // Phone microphone playback endpoint and application input.
	Playback, Capture string // Application playback endpoint and computer capture source.
	modules           []ownedModule
}

func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "pactl", args...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	b, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("pactl %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(b)), nil
}
func Create() (*Device, error) { return CreateFor(true, false) }
func moduleOwner() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
func CreateFor(microphone, playback bool) (*Device, error) {
	owner, err := moduleOwner()
	if err != nil {
		return nil, err
	}
	return createFor(microphone, playback, owner)
}
func createFor(microphone, playback bool, owner string) (*Device, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("automatic virtual audio setup requires Linux PulseAudio/PipeWire")
	}
	d := &Device{}
	existing, err := run("list", "short", "sinks")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(existing, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && ((microphone && fields[1] == "rmic_sink") || (playback && fields[1] == "rmic_computer")) {
			return nil, fmt.Errorf("%s already exists; close the other RMic instance, or select existing devices with --virtual=off", fields[1])
		}
	}
	load := func(args ...string) error {
		if owner != "" {
			for i, arg := range args {
				if strings.HasPrefix(arg, "sink_properties=") || strings.HasPrefix(arg, "source_properties=") {
					key, value, _ := strings.Cut(arg, "=")
					args[i] = key + "=\"" + value + " rmic.owner=" + owner + "\""
				}
			}
		}
		id, err := run(append([]string{"load-module"}, args...)...)
		if err == nil {
			d.modules = append(d.modules, ownedModule{id, args[0], strings.Join(args[1:], " ")})
		}
		return err
	}
	if microphone {
		d.Output = "RMic_Virtual_Output"
		d.Input = "RMic_Virtual_Microphone"
		err = load("module-null-sink", "sink_name=rmic_sink", "rate=48000", "channels=2", "sink_properties=device.description="+d.Output)
		if err == nil {
			err = load("module-remap-source", "master=rmic_sink.monitor", "source_name=rmic_mic", "source_properties=device.description="+d.Input)
		}
	}
	if err == nil && playback {
		d.Playback = "RMic_Computer_Playback"
		d.Capture = "RMic_Computer_Capture"
		err = load("module-null-sink", "sink_name=rmic_computer", "rate=48000", "channels=2", "sink_properties=device.description="+d.Playback)
		if err == nil {
			err = load("module-remap-source", "master=rmic_computer.monitor", "source_name=rmic_computer_source", "source_properties=device.description="+d.Capture)
		}
	}
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}
func (d *Device) Close() error {
	if len(d.modules) == 0 {
		return nil
	}
	var failures []string
	var remaining []ownedModule
	data, err := run("list", "short", "modules")
	if err != nil {
		return fmt.Errorf("check virtual device ownership: %w", err)
	}
	for i := len(d.modules) - 1; i >= 0; i-- {
		owned := d.modules[i]
		for _, line := range strings.Split(data, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) >= 3 && fields[0] == owned.ID && fields[1] == owned.Name && fields[2] == owned.Argument {
				if _, err := run("unload-module", owned.ID); err != nil {
					failures = append(failures, err.Error())
					remaining = append(remaining, owned)
				}
			}
		}
	}
	d.modules = remaining
	if len(failures) > 0 {
		return fmt.Errorf("virtual device cleanup: %s", strings.Join(failures, "; "))
	}
	return nil
}
