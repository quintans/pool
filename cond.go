package pool

import (
	"context"
	"sync"
)

type Cond struct {
	mutex  sync.Mutex
	ch     chan struct{}
	closed bool
}

func NewCond() *Cond {
	return &Cond{
		ch: make(chan struct{}, 0),
	}
}

// Wait waits for the condition to be signaled or for the context to be cancelled.
// If the context is cancelled, it returns false. Otherwise, it returns true.
func (s *Cond) Wait(ctx context.Context) error {
	s.mutex.Lock()

	if s.closed {
		s.ch = make(chan struct{}, 0)
		s.closed = false
	}
	ch := s.ch

	s.mutex.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// Broadcast signals the condition to wake up all goroutine waiting on it.
func (s *Cond) Broadcast() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.ch)
}
