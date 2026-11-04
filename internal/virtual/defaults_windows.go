//go:build windows

package virtual

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ole32 = windows.NewLazySystemDLL("ole32.dll")
var coCreateInstance = ole32.NewProc("CoCreateInstance")
var coInitializeEx = ole32.NewProc("CoInitializeEx")
var coUninitialize = ole32.NewProc("CoUninitialize")
var coTaskMemFree = ole32.NewProc("CoTaskMemFree")

type comObject struct{ table *[15]uintptr }

//go:uintptrescapes
func (object *comObject) call(index int, args ...uintptr) uintptr {
	values := append([]uintptr{uintptr(unsafe.Pointer(object))}, args...)
	result, _, _ := syscall.SyscallN(object.table[index], values...)
	return result
}
func (object *comObject) release() {
	if object != nil {
		object.call(2)
	}
}
func failedHRESULT(result uintptr) bool { return uint32(result)&0x80000000 != 0 }
func createCOM(class, interfac string) (*comObject, error) {
	classID, err := windows.GUIDFromString(class)
	if err != nil {
		return nil, err
	}
	interfaceID, err := windows.GUIDFromString(interfac)
	if err != nil {
		return nil, err
	}
	var object *comObject
	result, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&classID)), 0, 1, uintptr(unsafe.Pointer(&interfaceID)), uintptr(unsafe.Pointer(&object)))
	if failedHRESULT(result) {
		return nil, fmt.Errorf("CoCreateInstance HRESULT 0x%08x", uint32(result))
	}
	return object, nil
}

type defaultEndpoint struct {
	flow, role uint32
	id         string
}
type audioDefaults struct {
	enumerator, policy *comObject
	endpoints          []defaultEndpoint
	uninitialize       bool
}

func (defaults *audioDefaults) close() {
	defaults.policy.release()
	defaults.enumerator.release()
	if defaults.uninitialize {
		coUninitialize.Call()
	}
}
func (defaults *audioDefaults) current(flow, role uint32) (string, error) {
	var device *comObject
	result := defaults.enumerator.call(4, uintptr(flow), uintptr(role), uintptr(unsafe.Pointer(&device)))
	if uint32(result) == 0x80070490 {
		return "", nil
	} // No default device existed before installation.
	if failedHRESULT(result) {
		return "", fmt.Errorf("GetDefaultAudioEndpoint HRESULT 0x%08x", uint32(result))
	}
	defer device.release()
	var id *uint16
	result = device.call(5, uintptr(unsafe.Pointer(&id)))
	if failedHRESULT(result) {
		return "", fmt.Errorf("IMMDevice.GetId HRESULT 0x%08x", uint32(result))
	}
	defer coTaskMemFree.Call(uintptr(unsafe.Pointer(id)))
	return windows.UTF16PtrToString(id), nil
}

// The caller holds its OS thread until close: COM objects and apartment state
// must stay on that thread while the elevated vendor installer runs.
func captureAudioDefaults() (*audioDefaults, error) {
	defaults := &audioDefaults{}
	result, _, _ := coInitializeEx.Call(0, 2)
	if failedHRESULT(result) && uint32(result) != 0x80010106 {
		return nil, fmt.Errorf("CoInitializeEx HRESULT 0x%08x", uint32(result))
	}
	defaults.uninitialize = !failedHRESULT(result)
	var err error
	defaults.enumerator, err = createCOM("{BCDE0395-E52F-467C-8E3D-C4579291692E}", "{A95664D2-9614-4F35-A746-DE8DB63617E6}")
	if err == nil {
		defaults.policy, err = createCOM("{870AF99C-171D-4F9E-AF0D-E63DF40C2BC9}", "{F8679F50-850A-41CF-9C72-430F290290C8}")
	}
	if err != nil {
		defaults.close()
		return nil, err
	}
	for flow := uint32(0); flow < 2; flow++ {
		for role := uint32(0); role < 3; role++ {
			id, err := defaults.current(flow, role)
			if err != nil {
				defaults.close()
				return nil, err
			}
			if id != "" {
				defaults.endpoints = append(defaults.endpoints, defaultEndpoint{flow, role, id})
			}
		}
	}
	return defaults, nil
}
func (defaults *audioDefaults) restore() error {
	var problems []error
	for _, endpoint := range defaults.endpoints {
		current, err := defaults.current(endpoint.flow, endpoint.role)
		if err == nil && current == endpoint.id {
			continue
		}
		id, err := windows.UTF16PtrFromString(endpoint.id)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		result := defaults.policy.call(13, uintptr(unsafe.Pointer(id)), uintptr(endpoint.role))
		if failedHRESULT(result) {
			problems = append(problems, fmt.Errorf("恢复默认声卡失败，角色 %d/%d，HRESULT 0x%08x", endpoint.flow, endpoint.role, uint32(result)))
		}
	}
	return errors.Join(problems...)
}
