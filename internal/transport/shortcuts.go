package transport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"github.com/gorilla/websocket"
)

type shortcutButton struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}
type ShortcutStatus struct {
	Revision uint64           `json:"revision"`
	Buttons  []shortcutButton `json:"buttons"`
	Error    string           `json:"error,omitempty"`
	Pressed  []string         `json:"pressed,omitempty"`
	Busy     bool             `json:"busy"`
}
type shortcutTicket struct {
	token, session string
	revision       uint64
	expires        time.Time
}
type shortcutClient struct {
	conn     *websocket.Conn
	writeMu  sync.Mutex
	lastBeat time.Time // protected by shortcutControl.mu
}

func (c *shortcutClient) reply(v any) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(time.Second))
	if c.conn.WriteJSON(v) != nil {
		_ = c.conn.Close()
	}
}

type shortcutControl struct {
	mu        sync.Mutex
	config    shortcuts.Config
	revision  uint64
	engine    *shortcuts.Engine
	tickets   map[string]shortcutTicket
	owner     *shortcutClient
	lastError string
}

func newShortcutControl() *shortcutControl {
	return &shortcutControl{revision: 1, engine: shortcuts.NewEngine(shortcuts.NewNative()), tickets: map[string]shortcutTicket{}}
}
func WithShortcuts(config shortcuts.Config) Option {
	return func(s *Server) { s.shortcuts.config = config }
}
func (c *shortcutControl) stopLocked() {
	clear(c.tickets)
	if c.owner != nil {
		_ = c.owner.conn.Close()
		c.owner = nil
	}
	if err := c.engine.ReleaseAll(); err != nil {
		c.lastError = err.Error()
	}
}
func (s *Server) StopShortcuts() {
	s.shortcuts.mu.Lock()
	defer s.shortcuts.mu.Unlock()
	s.shortcuts.stopLocked()
}
func (s *Server) ConfigureShortcuts(config shortcuts.Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if config.Enabled && !shortcuts.Supported() {
		return fmt.Errorf("快捷按钮目前仅支持 Windows 电脑")
	}
	c := s.shortcuts
	c.mu.Lock()
	defer c.mu.Unlock()
	if config != c.config {
		c.stopLocked()
		if err := c.engine.SetKeyInterval(config.KeyIntervalMS); err != nil {
			return err
		}
		c.config = config
		c.revision++
	}
	return nil
}
func (c *shortcutControl) status() *ShortcutStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.config.Enabled {
		return nil
	}
	status := &ShortcutStatus{Revision: c.revision, Error: c.lastError}
	status.Pressed, status.Busy = c.engine.State()
	for i, b := range c.config.Buttons {
		if b.Name != "" {
			status.Buttons = append(status.Buttons, shortcutButton{strconv.Itoa(i + 1), b.Name, b.Mode})
		}
	}
	return status
}
func (s *Server) shortcutRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/shortcuts/ticket", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c := s.shortcuts
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.config.Enabled {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.closed || s.active == nil || s.active.id != r.Header.Get("X-RMic-Session") || !constantToken(s.active.credential, token) {
			http.Error(w, "请先连接音频会话", 409)
			return
		}
		revision, err := strconv.ParseUint(r.Header.Get("X-RMic-Shortcuts"), 10, 64)
		if err != nil || revision != c.revision {
			http.Error(w, "快捷按钮配置已变化", 409)
			return
		}
		now := time.Now()
		for ticket, entry := range c.tickets {
			if now.After(entry.expires) {
				delete(c.tickets, ticket)
			}
		}
		if c.owner != nil || len(c.tickets) >= 16 {
			http.Error(w, "快捷按钮控制已被占用", 409)
			return
		}
		ticket, err := RandomToken()
		if err != nil {
			http.Error(w, "cannot create ticket", 500)
			return
		}
		c.tickets[ticket] = shortcutTicket{token, s.active.id, c.revision, now.Add(10 * time.Second)}
		writeJSON(w, map[string]string{"ticket": ticket})
	}))
	mux.HandleFunc("GET /api/shortcuts/control", s.connectShortcuts)
}
func (s *Server) connectShortcuts(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", 403)
		return
	}
	c := s.shortcuts
	// All locks shared with audio lifecycle are acquired in server -> control order.
	s.mu.Lock()
	c.mu.Lock()
	ticket, ok := c.tickets[r.URL.Query().Get("ticket")]
	delete(c.tickets, r.URL.Query().Get("ticket"))
	valid := ok && time.Now().Before(ticket.expires) && c.config.Enabled && ticket.revision == c.revision && !s.closed && s.active != nil && s.active.id == ticket.session && constantToken(s.active.credential, ticket.token) && s.validCredential(ticket.token) && c.owner == nil
	if !valid {
		c.mu.Unlock()
		s.mu.Unlock()
		http.Error(w, "控制连接已失效或被占用", 403)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin, HandshakeTimeout: time.Second}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.mu.Unlock()
		s.mu.Unlock()
		return
	}
	client := &shortcutClient{conn: conn, lastBeat: time.Now()}
	c.owner = client
	config := c.config
	c.mu.Unlock()
	s.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.owner == client {
			c.stopLocked()
		}
		_ = conn.Close()
	}()
	// A blocked writer must not postpone key release until another read occurs.
	leaseDone := make(chan struct{})
	defer close(leaseDone)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-leaseDone:
				return
			case <-ticker.C:
				c.mu.Lock()
				if c.owner != client {
					c.mu.Unlock()
					return
				}
				expired := time.Since(client.lastBeat) > 1500*time.Millisecond
				if expired {
					c.stopLocked()
				}
				c.mu.Unlock()
				if expired {
					return
				}
			}
		}
	}()
	conn.SetReadLimit(2048)
	lastBeat := time.Now()
	var lastSeq uint64
	window := time.Now()
	count := 0
	client.reply(map[string]any{"type": "ready"})
	for {
		conn.SetReadDeadline(lastBeat.Add(1500 * time.Millisecond))
		var message struct {
			Type     string `json:"type"`
			Seq      uint64 `json:"seq"`
			Revision uint64 `json:"config_revision"`
			Button   string `json:"button_id"`
			HoldID   string `json:"hold_id"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		now := time.Now()
		if now.Sub(window) >= time.Second {
			window = now
			count = 0
		}
		count++
		if count > 100 || message.Seq <= lastSeq || now.Sub(lastBeat) > 1500*time.Millisecond {
			return
		}
		lastSeq = message.Seq
		s.mu.Lock()
		c.mu.Lock()
		valid = c.owner == client && c.config.Enabled && c.revision == ticket.revision && s.active != nil && s.active.id == ticket.session && s.validCredential(ticket.token)
		s.mu.Unlock()
		if !valid {
			c.mu.Unlock()
			return
		}
		var done <-chan error
		err = nil
		switch message.Type {
		case "heartbeat":
			lastBeat = now
			client.lastBeat = now
		case "release_all":
			err = c.engine.ReleaseAll()
		case "button.hold_end":
			err = c.engine.End(message.HoldID)
		case "button.run", "button.hold_begin":
			index, parseErr := strconv.Atoi(message.Button)
			if parseErr != nil || index < 1 || index > 3 || config.Buttons[index-1].Name == "" || message.Revision != c.revision {
				err = fmt.Errorf("按钮不存在或配置已变化")
				break
			}
			if message.Type == "button.run" {
				done, err = c.engine.Run(config.Buttons[index-1])
			} else if message.HoldID == "" || len(message.HoldID) > 64 {
				err = fmt.Errorf("保持操作标识无效")
			} else {
				err = c.engine.Begin(message.HoldID, config.Buttons[index-1])
			}
		default:
			err = fmt.Errorf("未知控制操作")
		}
		if err != nil {
			c.lastError = err.Error()
		} else if message.Type != "heartbeat" {
			c.lastError = ""
		}
		c.mu.Unlock()
		response := map[string]any{"type": "ack", "seq": message.Seq}
		if err != nil {
			response["error"] = err.Error()
		}
		client.reply(response)
		if done != nil {
			seq := message.Seq
			go func() {
				result := <-done
				c.mu.Lock()
				if c.owner == client && result != nil {
					c.lastError = result.Error()
				}
				c.mu.Unlock()
				response := map[string]any{"type": "complete", "seq": seq}
				if result != nil {
					response["error"] = result.Error()
				}
				client.reply(response)
			}()
		}
	}
}
