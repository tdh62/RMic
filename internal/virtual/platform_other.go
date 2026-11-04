//go:build !windows

package virtual

import "fmt"

func manageWindowsSteam(string, string) (string, error) {
	return "", fmt.Errorf("Steam 驱动管理仅支持 Windows")
}
func ApplySteamDriver(string) (string, error) {
	return "", fmt.Errorf("Steam 驱动管理仅支持 Windows")
}

func manageWindowsCable(string, string, string) (string, error) {
	return "", fmt.Errorf("VB-CABLE 驱动管理仅支持 Windows")
}
func manageWindows(string, string) (string, error) {
	return "", fmt.Errorf("Windows driver management requires Windows")
}
