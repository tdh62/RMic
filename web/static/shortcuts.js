'use strict';

// Buttons and their actions are configured on the computer. Only slot IDs travel
// from this UI; a reconnect never replays an input operation.
class RmicShortcuts {
  constructor(api) {
    this.api = api;
    this.panel = document.getElementById('shortcut-controls');
    this.buttons = document.getElementById('shortcut-buttons');
    this.notice = document.getElementById('shortcut-state');
    this.epoch = 0;
    this.seq = 0;
    this.holds = new Map();
    this.pending = new Map();
    document.getElementById('shortcut-release').onclick = () => this.close('已停止控制');
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState !== 'visible') this.close('页面已隐藏，已停止控制');
    });
    window.addEventListener('blur', () => this.close('页面失去焦点，已停止控制'));
    window.addEventListener('pagehide', () => this.close());
  }
  close(message = '连接音频后可使用') {
    this.epoch++;
    clearInterval(this.heartbeat);
    this.ws?.close();
    this.ws = null;
    this.connecting = false;
    this.ready = false;
    this.busy = false;
    this.holds.clear();
    this.pending.clear();
    this.notice.textContent = message;
    this.renderState();
  }
  renderState() {
    for (const button of this.buttons.children) {
      button.disabled = !this.ready || this.busy || (button.dataset.mode === 'tap' && this.holds.size > 0);
      const active = [...this.holds.values()].some(hold => hold.button === button && hold.ack);
      button.classList.toggle('holding', active);
      button.setAttribute('aria-pressed', String(active));
    }
  }
  send(type, fields = {}, pending) {
    if (!this.ready || this.ws?.readyState !== WebSocket.OPEN) return 0;
    const seq = ++this.seq;
    if (pending) this.pending.set(seq, pending);
    this.ws.send(JSON.stringify({ type, seq, config_revision: this.revision, ...fields }));
    return seq;
  }
  begin(button, pointer) {
    if (!this.ready || this.busy || this.holds.has(pointer)) return;
    const id = `${this.epoch}-${++this.holdCounter}`;
    const hold = { id, button, ack: false };
    this.holds.set(pointer, hold);
    if (!this.send('button.hold_begin', { button_id: button.dataset.id, hold_id: id }, { kind: 'hold', hold })) {
      this.holds.delete(pointer);
    }
    this.renderState();
  }
  end(pointer) {
    const hold = this.holds.get(pointer);
    if (!hold) return;
    this.holds.delete(pointer);
    this.send('button.hold_end', { hold_id: hold.id });
    this.renderState();
  }
  build(buttons) {
    this.buttons.replaceChildren();
    for (const slot of buttons) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'secondary shortcut-button';
      button.dataset.id = slot.id;
      button.dataset.mode = slot.mode;
      const label = document.createElement('strong');
      label.textContent = slot.name;
      const hint = document.createElement('small');
      hint.textContent = slot.mode === 'hold' ? '按住保持 · 松开释放' : '点击执行';
      button.append(label, hint);
      if (slot.mode === 'hold') {
        button.addEventListener('pointerdown', event => {
          if (event.button !== 0 || button.disabled) return;
          event.preventDefault();
          button.setPointerCapture(event.pointerId);
          this.begin(button, event.pointerId);
        });
        for (const name of ['pointerup', 'pointercancel', 'lostpointercapture']) {
          button.addEventListener(name, event => this.end(event.pointerId));
        }
        button.addEventListener('keydown', event => {
          if (![' ', 'Enter'].includes(event.key)) return;
          event.preventDefault();
          if (!event.repeat) this.begin(button, `key-${slot.id}-${event.key}`);
        });
        button.addEventListener('keyup', event => {
          if (![' ', 'Enter'].includes(event.key)) return;
          event.preventDefault();
          this.end(`key-${slot.id}-${event.key}`);
        });
        button.addEventListener('blur', () => {
          for (const [pointer, hold] of this.holds) if (hold.button === button) this.end(pointer);
        });
        button.addEventListener('contextmenu', event => event.preventDefault());
      } else {
        button.onclick = () => {
          if (!this.ready || this.busy || this.holds.size) return;
          this.busy = true;
          this.send('button.run', { button_id: slot.id }, { kind: 'run' });
          this.notice.textContent = '执行中…';
          this.renderState();
        };
      }
      this.buttons.append(button);
    }
    this.renderState();
  }
  update(config, session) {
    this.panel.hidden = !config;
    if (!config) {
      this.close();
      this.revision = 0;
      this.session = '';
      return;
    }
    if (this.revision !== config.revision || this.session !== session) {
      this.close();
      this.revision = config.revision;
      this.session = session;
      this.build(config.buttons);
    }
    if (config.error) this.notice.textContent = config.error;
    if (!session || this.ws || this.connecting || document.visibilityState !== 'visible' || Date.now() < (this.retryAt || 0)) return;
    const epoch = this.epoch;
    this.connecting = true;
    this.holdCounter = 0;
    this.api('/api/shortcuts/ticket', {
      method: 'POST',
      headers: { 'X-RMic-Session': session, 'X-RMic-Shortcuts': String(config.revision) }
    }).then(result => {
      if (epoch !== this.epoch || document.visibilityState !== 'visible') return;
      const url = new URL('/api/shortcuts/control', location.href);
      url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      url.searchParams.set('ticket', result.ticket);
      const ws = new WebSocket(url);
      this.ws = ws;
      ws.onmessage = event => {
        if (epoch !== this.epoch) return;
        const response = JSON.parse(event.data);
        if (response.type === 'ready') {
          this.ready = true;
          this.notice.textContent = '作用于电脑当前窗口';
          this.heartbeat = setInterval(() => this.send('heartbeat'), 250);
          this.send('heartbeat');
        } else if (response.type === 'ack') {
          const request = this.pending.get(response.seq);
          this.pending.delete(response.seq);
          if (response.error) {
            // Injection failures can also release other shared keys on the
            // computer. End this control session so every local latch is reset.
            this.close(response.error);
            return;
          } else if (request?.kind === 'hold') {
            request.hold.ack = true;
          }
        } else if (response.type === 'complete') {
          this.busy = false;
          this.notice.textContent = response.error || '执行完成';
        }
        this.renderState();
      };
      ws.onclose = () => {
        if (epoch !== this.epoch) return;
        this.close('控制连接已断开，按键已停止');
        this.retryAt = Date.now() + 1500;
      };
      ws.onerror = () => ws.close();
    }).catch(error => {
      if (epoch !== this.epoch) return;
      this.notice.textContent = error.message;
      this.retryAt = Date.now() + 3000;
    }).finally(() => {
      if (epoch === this.epoch) this.connecting = false;
    });
  }
}
