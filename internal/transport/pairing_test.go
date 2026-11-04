package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cnb.cool/tdh6/RMic/internal/pairing"
)

func TestPairingApprovalAndPersistentCredentialsHTTP(t *testing.T) {
	manager, err := pairing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(&testOutput{}, "test", 0, WithPairing(manager))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	web := httptest.NewTLSServer(server.Handler())
	defer web.Close()
	requireStatus(t, request(t, web, "GET", "/api/status", "", nil), 401)
	requireStatus(t, request(t, web, "POST", "/api/pair/request", "", []byte(`{"name":"phone","remember":true}`), http.Header{"Origin": {"https://evil.example"}}), 403)
	response := request(t, web, "POST", "/api/pair/request", "", []byte(`{"name":"phone","remember":true}`))
	var ticket pairing.Ticket
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	body, _ := json.Marshal(map[string]string{"id": ticket.ID, "secret": "wrong"})
	requireStatus(t, request(t, web, "POST", "/api/pair/status", "", body), 404)
	requireStatus(t, request(t, web, "POST", "/api/pair/approve", "", []byte(`{}`)), 405)
	if err = manager.Decide(ticket.ID, true); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"id": ticket.ID, "secret": ticket.Secret})
	response = request(t, web, "POST", "/api/pair/status", "", body)
	var result pairing.Result
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	requireStatus(t, request(t, web, "GET", "/api/status", result.Token, nil), 200)
	if err = manager.Revoke(result.DeviceID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, web, "GET", "/api/status", result.Token, nil), 401)
	requireStatus(t, request(t, web, "POST", "/api/pair/status", "", body), 404)
}
func TestBootstrapRememberAndMobileForget(t *testing.T) {
	manager, _ := pairing.Open(t.TempDir())
	server, err := NewServer(&testOutput{}, "test", 0, WithPairing(manager))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	web := httptest.NewTLSServer(server.Handler())
	defer web.Close()
	response := request(t, web, "POST", "/api/pair/remember", server.Token(), []byte(`{"name":"remembered"}`))
	var result pairing.Result
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if result.Token == server.Token() || !result.Remember {
		t.Fatal("bootstrap credential not exchanged for device credential")
	}
	response = request(t, web, "POST", "/api/pair/remember", result.Token, []byte(`{"name":"duplicate"}`))
	requireStatus(t, response, 200)
	if len(manager.Devices()) != 1 {
		t.Fatal("remembered device delegated new authorization")
	}
	requireStatus(t, request(t, web, "POST", "/api/pair/forget", result.Token, nil), 204)
	requireStatus(t, request(t, web, "GET", "/api/status", result.Token, nil), 401)
}
