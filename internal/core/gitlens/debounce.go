package gitlens

import (
	"sync"
	"time"
)

// Debouncer manages timed debounced execution to ensure zero editor lag.
type Debouncer struct {
	mu     sync.Mutex
	delay  time.Duration
	timers map[string]*time.Timer
}

// NewDebouncer creates a Debouncer with the specified quiet period.
func NewDebouncer(delay time.Duration) *Debouncer {
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	return &Debouncer{
		delay:  delay,
		timers: make(map[string]*time.Timer),
	}
}

// Debounce delays the execution of fn by the configured delay duration.
// If Debounce is called again with the same key before the timer fires,
// the previous execution is cancelled and the timer is reset.
func (d *Debouncer) Debounce(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.timers[key]; ok && t != nil {
		t.Stop()
	}

	d.timers[key] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}

// Cancel terminates any pending execution for the given key.
func (d *Debouncer) Cancel(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.timers[key]; ok && t != nil {
		t.Stop()
		delete(d.timers, key)
	}
}

// CancelAll cancels all active timers.
func (d *Debouncer) CancelAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for k, t := range d.timers {
		if t != nil {
			t.Stop()
		}
		delete(d.timers, k)
	}
}

// Delay returns the current debounce duration.
func (d *Debouncer) Delay() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.delay
}

// SetDelay updates the debounce duration.
func (d *Debouncer) SetDelay(delay time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	d.delay = delay
}
