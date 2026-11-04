package virtual

import "strings"

var steamDevices = []struct{ Name, HardwareID string }{
	{"SteamStreamingMicrophone", `ROOT\SteamStreamingMicrophone`},
	{"SteamStreamingSpeakers", `ROOT\SteamStreamingSpeakers`},
}

func steamHardwareIndex(ids []string) int {
	for _, id := range ids {
		for index, device := range steamDevices {
			if strings.EqualFold(id, device.HardwareID) {
				return index
			}
		}
	}
	return -1
}
