// Package pairing manages local approval and revocable, remembered devices.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var ErrUnknown = errors.New("配对申请不存在、已过期或凭证无效")
var ErrRate = errors.New("配对申请过于频繁，请稍后重试")

type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Remember bool      `json:"remember"`
}
type record struct {
	Device
	Hash string `json:"token_hash"`
}
type Request struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	Code     string    `json:"code"`
	Expires  time.Time `json:"expires"`
	Remember bool      `json:"remember"`
}
type pending struct {
	Request
	SecretHash string
	State      string
	Token      string
	DeviceID   string
}
type Ticket struct {
	Request
	Secret string `json:"secret"`
}
type Result struct {
	State    string `json:"state"`
	Token    string `json:"token,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Remember bool   `json:"remember"`
}
type Manager struct {
	mu      sync.Mutex
	path    string
	devices []record
	pending map[string]*pending
	rate    map[string]time.Time
}

func random() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func hash(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }
func equal(a, b string) bool   { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func label(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	runes := []rune(strings.TrimSpace(name))
	if len(runes) > 40 {
		runes = runes[:40]
	}
	if len(runes) == 0 {
		return "移动设备"
	}
	return string(runes)
}
func Open(dir string) (*Manager, error) {
	m := &Manager{path: filepath.Join(dir, "trusted-devices.json"), pending: map[string]*pending{}, rate: map[string]time.Time{}}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var state struct {
		Version int      `json:"version"`
		Devices []record `json:"devices"`
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("读取授权记录：%w", err)
	}
	if state.Version != 1 || len(state.Devices) > 64 {
		return nil, fmt.Errorf("不支持的授权记录")
	}
	seen := map[string]bool{}
	for _, device := range state.Devices {
		value, err := hex.DecodeString(device.Hash)
		id, idErr := hex.DecodeString(device.ID)
		if err != nil || len(value) != 32 || idErr != nil || len(id) != 24 || seen[device.ID] || !device.Remember {
			return nil, fmt.Errorf("无效的授权记录")
		}
		seen[device.ID] = true
		device.Name = label(device.Name)
		m.devices = append(m.devices, device)
	}
	return m, nil
}
func (m *Manager) save() error {
	durable := make([]record, 0, len(m.devices))
	for _, device := range m.devices {
		if device.Remember {
			durable = append(durable, device)
		}
	}
	data, err := json.MarshalIndent(struct {
		Version int      `json:"version"`
		Devices []record `json:"devices"`
	}{1, durable}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(m.path), ".trusted-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), m.path)
}
func (m *Manager) prune() {
	now := time.Now()
	for id, p := range m.pending {
		if now.After(p.Expires) {
			delete(m.pending, id)
		}
	}
	for ip, at := range m.rate {
		if now.Sub(at) > 10*time.Second {
			delete(m.rate, ip)
		}
	}
}
func (m *Manager) Create(name, address string, remember bool) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	if len(m.pending) >= 32 || len(m.rate) >= 64 || time.Since(m.rate[address]) < 10*time.Second {
		return Ticket{}, ErrRate
	}
	id, err := random()
	if err != nil {
		return Ticket{}, err
	}
	secret, err := random()
	if err != nil {
		return Ticket{}, err
	}
	code, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return Ticket{}, err
	}
	request := Request{id, label(name), address, fmt.Sprintf("%06d", code.Int64()), time.Now().Add(2 * time.Minute), remember}
	m.pending[id] = &pending{Request: request, SecretHash: hash(secret), State: "pending"}
	m.rate[address] = time.Now()
	return Ticket{request, secret}, nil
}
func (m *Manager) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	var requests []Request
	for _, p := range m.pending {
		if p.State == "pending" {
			requests = append(requests, p.Request)
		}
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Expires.Before(requests[j].Expires) })
	return requests
}
func (m *Manager) Devices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	devices := make([]Device, len(m.devices))
	for i, d := range m.devices {
		devices[i] = d.Device
	}
	return devices
}
func (m *Manager) add(name string, remember bool) (record, string, error) {
	if len(m.devices) >= 64 {
		return record{}, "", fmt.Errorf("授权设备已达上限，请先撤销旧设备")
	}
	id, err := random()
	if err != nil {
		return record{}, "", err
	}
	token, err := random()
	if err != nil {
		return record{}, "", err
	}
	device := record{Device: Device{id, label(name), time.Now(), remember}, Hash: hash(token)}
	m.devices = append(m.devices, device)
	if remember {
		if err = m.save(); err != nil {
			m.devices = m.devices[:len(m.devices)-1]
			return record{}, "", err
		}
	}
	return device, token, nil
}

// Remember is reserved for already authenticated bootstrap-link clients.
func (m *Manager) Remember(name string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	device, token, err := m.add(name, true)
	return Result{"approved", token, device.ID, true}, err
}
func (m *Manager) Decide(id string, allow bool) error { return m.decide(id, allow, false) }
func (m *Manager) DecideOnce(id string) error         { return m.decide(id, true, true) }
func (m *Manager) decide(id string, allow, once bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	p := m.pending[id]
	if p == nil || p.State != "pending" {
		return ErrUnknown
	}
	if !allow {
		p.State = "denied"
		return nil
	}
	remember := p.Remember && !once
	device, token, err := m.add(p.Name, remember)
	if err != nil {
		return err
	}
	p.State = "approved"
	p.Remember = remember
	p.Token = token
	p.Expires = time.Now().Add(2 * time.Minute)
	p.DeviceID = device.ID
	return nil
}
func (m *Manager) Poll(id, secret string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	p := m.pending[id]
	if p == nil || !equal(hash(secret), p.SecretHash) {
		return Result{}, ErrUnknown
	}
	return Result{p.State, p.Token, p.DeviceID, p.Remember}, nil
}
func (m *Manager) KnownDevice(token string) (Device, bool) {
	if len(token) != 48 {
		return Device{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	digest := hash(token)
	for _, record := range m.devices {
		if equal(digest, record.Hash) {
			return record.Device, true
		}
	}
	return Device{}, false
}
func (m *Manager) Valid(token string) bool { _, ok := m.KnownDevice(token); return ok }
func (m *Manager) Revoke(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index, device := range m.devices {
		if device.ID == id {
			previous := append([]record(nil), m.devices...)
			m.devices = append(m.devices[:index], m.devices[index+1:]...)
			if err := m.save(); err != nil {
				m.devices = previous
				return err
			}
			for requestID, p := range m.pending {
				if p.State == "approved" && p.DeviceID == id {
					delete(m.pending, requestID)
				}
			}
			return nil
		}
	}
	return ErrUnknown
}
