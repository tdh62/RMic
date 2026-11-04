//go:build windows

package virtual

import (
	"strings"
	"testing"
)

func TestPublicDriverOperationsFailClosed(t *testing.T) {
	for _, action := range []string{"install", "uninstall"} {
		if _, err := manageWindowsCable(action, t.TempDir(), "vbcable"); err == nil || !strings.Contains(err.Error(), "未启用") {
			t.Fatalf("%s without config: %v", action, err)
		}
		for _, driver := range []string{"vbcable-a", "vbcable-b"} {
			if _, err := manageWindowsCable(action, t.TempDir(), driver); err == nil {
				t.Fatalf("%s %s accepted", driver, action)
			}
		}
		if _, err := ApplySteamDriver(action); err == nil {
			t.Fatal("legacy Steam helper accepted")
		}
	}
	for _, provider := range DriverProviders() {
		if !provider.ReadOnly || strings.HasPrefix(provider.Prefix, "cable-") {
			t.Fatalf("unexpected public provider: %+v", provider)
		}
	}
}
