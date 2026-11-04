package transport

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"cnb.cool/tdh6/RMic/internal/pairing"
)

func WithPairing(manager *pairing.Manager) Option  { return func(s *Server) { s.pairing = manager } }
func (s *Server) PairingManager() *pairing.Manager { return s.pairing }
func sameOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == "https://"+r.Host
}
func (s *Server) validCredential(token string) bool {
	return token != "" && (constantToken(token, s.token) || s.pairing != nil && s.pairing.Valid(token))
}
func (s *Server) pairingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pair/request", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		if s.pairing == nil {
			http.Error(w, "电脑未启用申请配对，请使用连接凭证", 503)
			return
		}
		var input struct {
			Name     string `json:"name"`
			Remember bool   `json:"remember"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input); err != nil {
			http.Error(w, "invalid pairing request", 400)
			return
		}
		address, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			address = r.RemoteAddr
		}
		ticket, err := s.pairing.Create(input.Name, address, input.Remember)
		if err != nil {
			code := 500
			if errors.Is(err, pairing.ErrRate) {
				code = 429
			}
			http.Error(w, err.Error(), code)
			return
		}
		writeJSON(w, ticket)
	})
	mux.HandleFunc("POST /api/pair/status", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		if s.pairing == nil {
			http.NotFound(w, r)
			return
		}
		var input struct {
			ID     string `json:"id"`
			Secret string `json:"secret"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
			http.Error(w, "invalid pairing status", 400)
			return
		}
		result, err := s.pairing.Poll(input.ID, input.Secret)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("POST /api/pair/forget", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if s.pairing != nil {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if device, ok := s.pairing.KnownDevice(token); ok {
				if err := s.pairing.Revoke(device.ID); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/pair/remember", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if s.pairing == nil {
			http.Error(w, "电脑未启用历史授权", 503)
			return
		}
		// This endpoint upgrades the deliberately shared bootstrap link only.
		// Remembered device tokens cannot silently create further trusted devices.
		if !constantToken(r.Header.Get("Authorization"), "Bearer "+s.token) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			device, ok := s.pairing.KnownDevice(token)
			if !ok {
				http.Error(w, "授权已失效", 401)
				return
			}
			writeJSON(w, pairing.Result{State: "approved", Token: token, DeviceID: device.ID, Remember: device.Remember})
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
			http.Error(w, "invalid device name", 400)
			return
		}
		result, err := s.pairing.Remember(input.Name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, result)
	}))
}
