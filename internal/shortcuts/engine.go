package shortcuts

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Backend interface {
	Key(string, bool) error
	Text(rune) error
}

// Engine serializes injection and owns only the keys pressed by this feature.
type Engine struct {
	keyInterval time.Duration
	mu          sync.Mutex
	backend     Backend
	holds       map[string][]string
	pressed     []string
	busy        bool
	cancel      context.CancelFunc
	generation  uint64
}

func (e *Engine) State() ([]string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.pressed...), e.busy
}
func NewEngine(backend Backend) *Engine {
	return &Engine{backend: backend, holds: map[string][]string{}}
}
func (e *Engine) SetKeyInterval(ms int) error {
	if err := validateKeyInterval(ms); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keyInterval = time.Duration(ms) * time.Millisecond
	return nil
}

// Emergency cleanup intentionally skips the normal inter-key delay.
func (e *Engine) releaseLocked() error {
	var first error
	for i := len(e.pressed) - 1; i >= 0; i-- {
		if err := e.backend.Key(e.pressed[i], false); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		e.pressed = append(e.pressed[:i], e.pressed[i+1:]...)
	}
	return first
}
func (e *Engine) resetLocked() error {
	e.generation++
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.busy = false
	clear(e.holds)
	return e.releaseLocked()
}
func (e *Engine) ReleaseAll() error { e.mu.Lock(); defer e.mu.Unlock(); return e.resetLocked() }
func (e *Engine) beginLocked(id string, keys []string) error {
	if _, exists := e.holds[id]; exists {
		return nil
	}
	e.holds[id] = keys
	sent := false
	for _, key := range keys {
		exists := false
		for _, pressed := range e.pressed {
			if pressed == key {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if sent && e.keyInterval > 0 {
			time.Sleep(e.keyInterval)
		}
		// Include an attempted key in cleanup even when the system reports failure.
		e.pressed = append(e.pressed, key)
		if err := e.backend.Key(key, true); err != nil {
			_ = e.resetLocked()
			return err
		}
		sent = true
	}
	return nil
}
func (e *Engine) endLocked(id string) error {
	delete(e.holds, id)
	desired := map[string]bool{}
	for _, keys := range e.holds {
		for _, key := range keys {
			desired[key] = true
		}
	}
	sent := false
	for i := len(e.pressed) - 1; i >= 0; i-- {
		key := e.pressed[i]
		if desired[key] {
			continue
		}
		if sent && e.keyInterval > 0 {
			time.Sleep(e.keyInterval)
		}
		if err := e.backend.Key(key, false); err != nil {
			_ = e.resetLocked()
			return err
		}
		e.pressed = append(e.pressed[:i], e.pressed[i+1:]...)
		sent = true
	}
	return nil
}
func (e *Engine) Begin(id string, b Button) error {
	steps, err := b.Steps()
	if err != nil {
		return err
	}
	if b.Name == "" || b.Mode != "hold" {
		return fmt.Errorf("按钮不是按住保持模式")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy || len(e.holds) >= 8 {
		return fmt.Errorf("正在执行或保持操作过多，请先释放")
	}
	if len(e.holds) == 0 && len(e.pressed) > 0 {
		if err := e.releaseLocked(); err != nil {
			return fmt.Errorf("仍有按键未释放：%w", err)
		}
	}
	return e.beginLocked(id, steps[0].Keys)
}
func (e *Engine) End(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy {
		return fmt.Errorf("固定输入正在执行")
	}
	return e.endLocked(id)
}

// Run reserves the engine synchronously, so a second request cannot queue work.
func (e *Engine) Run(b Button) (<-chan error, error) {
	steps, err := b.Steps()
	if err != nil {
		return nil, err
	}
	if b.Name == "" || b.Mode != "tap" {
		return nil, fmt.Errorf("按钮不是点击执行模式")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy || len(e.holds) > 0 || len(e.pressed) > 0 {
		return nil, fmt.Errorf("请等待执行完成并松开其他按钮")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	e.busy = true
	e.cancel = cancel
	generation := e.generation
	done := make(chan error, 1)
	go func() {
		var result error
		defer func() {
			cancel()
			e.mu.Lock()
			if e.generation == generation {
				if err := e.resetLocked(); result == nil {
					result = err
				}
			}
			e.mu.Unlock()
			done <- result
			close(done)
		}()
		inject := func(f func() error) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			if e.generation != generation {
				return context.Canceled
			}
			return f()
		}
		wait := func(d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
		for _, step := range steps {
			if len(step.Keys) > 0 {
				if result = inject(func() error { return e.beginLocked("run", step.Keys) }); result != nil {
					return
				}
				if result = wait(30 * time.Millisecond); result != nil {
					return
				}
				if result = inject(func() error { return e.endLocked("run") }); result != nil {
					return
				}
			} else if step.WaitMS > 0 {
				if result = wait(time.Duration(step.WaitMS) * time.Millisecond); result != nil {
					return
				}
			} else {
				for _, r := range step.Text {
					if result = inject(func() error { return e.backend.Text(r) }); result != nil {
						return
					}
				}
			}
		}
	}()
	return done, nil
}
