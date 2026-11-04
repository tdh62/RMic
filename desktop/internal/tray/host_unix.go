//go:build linux || freebsd || openbsd || netbsd

package tray

import (
	"fmt"
	"github.com/godbus/dbus/v5"
)

func available() error {
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer bus.Close()
	var owned bool
	if err := bus.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owned); err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("desktop has no StatusNotifier tray host")
	}
	return nil
}
