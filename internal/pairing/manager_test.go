package pairing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovalRememberRestartAndRevoke(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := m.Create("我的手机\x1b\n", "100.101.102.103", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticket.Code) != 6 || m.Requests()[0].Name != "我的手机" {
		t.Fatalf("unsafe/unreadable request: %+v", ticket)
	}
	if _, err = m.Poll(ticket.ID, "wrong"); err == nil {
		t.Fatal("polling secret not checked")
	}
	result, err := m.Poll(ticket.ID, ticket.Secret)
	if err != nil || result.State != "pending" || result.Token != "" {
		t.Fatal("credential available before approval")
	}
	if err = m.Decide(ticket.ID, true); err != nil {
		t.Fatal(err)
	}
	result, err = m.Poll(ticket.ID, ticket.Secret)
	if err != nil || result.State != "approved" || !m.Valid(result.Token) {
		t.Fatal("approved request not usable")
	}
	data, err := os.ReadFile(filepath.Join(dir, "trusted-devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), result.Token) || strings.Contains(string(data), ticket.Secret) {
		t.Fatal("raw credentials persisted on PC")
	}
	restored, err := Open(dir)
	if err != nil || !restored.Valid(result.Token) {
		t.Fatal("remembered device not restored")
	}
	if err = restored.Revoke(result.DeviceID); err != nil || restored.Valid(result.Token) {
		t.Fatal("revoke did not invalidate token")
	}
	restored, err = Open(dir)
	if err != nil || restored.Valid(result.Token) {
		t.Fatal("revocation not persisted")
	}
	if _, err = restored.Poll(ticket.ID, ticket.Secret); err == nil {
		t.Fatal("pending approval recovered after restart")
	}
}
func TestDeniedExpiredOnceAndRateLimited(t *testing.T) {
	m, _ := Open(t.TempDir())
	ticket, _ := m.Create("手机", "1", true)
	if _, err := m.Create("spam", "1", true); err != ErrRate {
		t.Fatal("request flood not limited")
	}
	if err := m.Decide(ticket.ID, false); err != nil {
		t.Fatal(err)
	}
	result, _ := m.Poll(ticket.ID, ticket.Secret)
	if result.State != "denied" || result.Token != "" {
		t.Fatal("denied client received credential")
	}
	expired, _ := m.Create("旧手机", "2", true)
	m.pending[expired.ID].Expires = time.Now().Add(-time.Second)
	if err := m.Decide(expired.ID, true); err != ErrUnknown {
		t.Fatal("expired request approved")
	}
	once, _ := m.Create("一次使用", "3", true)
	if err := m.DecideOnce(once.ID); err != nil {
		t.Fatal(err)
	}
	result, _ = m.Poll(once.ID, once.Secret)
	if result.Remember || !m.Valid(result.Token) {
		t.Fatal("once grant not usable or became persistent")
	}
	restarted, err := Open(filepath.Dir(m.path))
	if err != nil || restarted.Valid(result.Token) {
		t.Fatal("once grant survived restart")
	}
}
func TestFailedPersistenceDoesNotGrantOrLoseRevocation(t *testing.T) {
	dir := t.TempDir()
	m, _ := Open(dir)
	ticket, _ := m.Create("手机", "1", true)
	m.path = filepath.Join(dir, "blocked")
	if err := os.Mkdir(m.path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Decide(ticket.ID, true); err == nil {
		t.Fatal("grant claimed success without saving")
	}
	if len(m.Devices()) != 0 {
		t.Fatal("failed grant left usable authorization")
	}
	m.path = filepath.Join(dir, "trusted-devices.json")
	if err := m.Decide(ticket.ID, true); err != nil {
		t.Fatal(err)
	}
	result, _ := m.Poll(ticket.ID, ticket.Secret)
	m.path = filepath.Join(dir, "blocked")
	if err := m.Revoke(result.DeviceID); err == nil || !m.Valid(result.Token) {
		t.Fatal("failed revoke lost active record")
	}
}
func TestCorruptStateIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "trusted-devices.json")
	original := []byte(`{"version":1,"devices":[{"token_hash":"bad"}]}`)
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt authorization silently reset")
	}
	current, _ := os.ReadFile(file)
	if string(current) != string(original) {
		t.Fatal("corrupt file overwritten")
	}
	var valid any
	if err := json.Unmarshal(current, &valid); err != nil {
		t.Fatal(err)
	}
}
