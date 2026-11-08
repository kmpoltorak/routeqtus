package postgres

import (
	"context"
	"sync/atomic"
	"time"
)

// Health is database readiness sampled outside HTTP request handling.
type Health struct {
	ctx       context.Context
	cancel    context.CancelFunc
	available atomic.Bool
	done      chan struct{}
}

func (s *Store) WatchHealth(parent context.Context, interval time.Duration) *Health {
	ctx, cancel := context.WithCancel(parent)
	h := &Health{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			h.available.Store(s.Ping(ctx) == nil)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return h
}
func (h *Health) Ready() bool { return h.ctx.Err() == nil && h.available.Load() }
func (h *Health) Close()      { h.cancel(); <-h.done }
