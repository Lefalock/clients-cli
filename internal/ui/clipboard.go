// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"errors"
	"sync"
	"time"
)

type SystemClipboard struct {
	mu         sync.Mutex
	timer      *time.Timer
	ttl        time.Duration
	generation uint64
	value      string
	pending    bool
	closed     bool
	closeErr   error
	read       func() (string, error)
	write      func(string) error
}

func NewSystemClipboard(ttl time.Duration) *SystemClipboard {
	read, write := systemClipboardAccess()
	return &SystemClipboard{ttl: ttl, read: read, write: write}
}

func (c *SystemClipboard) Copy(value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("clipboard is closed")
	}
	if err := c.write(value); err != nil {
		return err
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.generation++
	c.value, c.pending = value, true
	generation := c.generation
	c.timer = time.AfterFunc(c.ttl, func() { c.expire(generation) })
	return nil
}

func (c *SystemClipboard) expire(generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || generation != c.generation {
		return
	}
	// Keep the value on failure so shutdown can retry the cleanup.
	_ = c.clearLocked()
}

// clearLocked preserves clipboard content replaced by another application.
// The OS read and write are separate operations; the mutex serializes only us.
func (c *SystemClipboard) clearLocked() error {
	if !c.pending {
		return nil
	}
	current, err := c.read()
	if err != nil {
		return errors.New("could not read clipboard during cleanup")
	}
	if current == c.value {
		if err := c.write(""); err != nil {
			return errors.New("could not clear clipboard")
		}
	}
	c.value, c.pending = "", false
	return nil
}

// Close prevents further copies and conditionally clears our last value.
// It is safe to call concurrently with Copy, the timer, and other Close calls.
// Repeated calls return the first result without touching the system clipboard.
func (c *SystemClipboard) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.closeErr = c.clearLocked()
	c.value, c.pending = "", false
	return c.closeErr
}

// TTL is the same immutable duration used for conditional clipboard cleanup.
func (c *SystemClipboard) TTL() time.Duration { return c.ttl }
