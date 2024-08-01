package pool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var ErrPoolClosed = errors.New("pool is closed")

type Option[T any] func(*Pool[T])

func JanitorSleep[T any](janitorSleep time.Duration) Option[T] {
	return func(p *Pool[T]) {
		p.janitorSleep = janitorSleep
	}
}

func IdleTimeout[T any](idleTimeout time.Duration) Option[T] {
	return func(p *Pool[T]) {
		p.idleTimeout = idleTimeout
	}
}

func BorrowTimeout[T any](borrowTimeout time.Duration) Option[T] {
	return func(p *Pool[T]) {
		p.borrowTimeout = borrowTimeout
	}
}

func Size[T any](size int) Option[T] {
	return func(p *Pool[T]) {
		if size < 1 {
			size = 1
		}
		p.size = size
	}
}

func MinIdle[T any](minIdle int) Option[T] {
	return func(p *Pool[T]) {
		if minIdle < 0 {
			minIdle = 0
		}
		p.minIdle = minIdle
	}
}

func ErrLogger[T any](errLogger func(ctx context.Context, err error, msg string)) Option[T] {
	return func(p *Pool[T]) {
		p.errLogger = errLogger
	}
}

func Validate[T any](validate func(context.Context, *T) (bool, error)) Option[T] {
	return func(p *Pool[T]) {
		p.validate = validate
	}
}

type Pool[T any] struct {
	cond             *Cond
	mutex            sync.Mutex
	errLogger        func(ctx context.Context, err error, msg string)
	janitorSleep     time.Duration
	idleTimeout      time.Duration
	borrowTimeout    time.Duration
	size             int
	minIdle          int
	locked, unlocked map[*T]time.Time
	create           func(context.Context) (*T, error)
	validate         func(context.Context, *T) (bool, error)
	expire           func(context.Context, *T)
	done             chan struct{}
	closed           bool
}

func New[T any](
	ctx context.Context,
	create func(context.Context) (*T, error),
	expire func(context.Context, *T),
	options ...Option[T],
) (*Pool[T], error) {
	p := &Pool[T]{
		cond: NewCond(),
		errLogger: func(ctx context.Context, err error, msg string) {
			slog.ErrorContext(ctx, msg, "error", err.Error())
		},
		create:        create,
		validate:      func(context.Context, *T) (bool, error) { return true, nil },
		expire:        expire,
		janitorSleep:  5 * time.Second,
		idleTimeout:   30 * time.Second,
		borrowTimeout: 30 * time.Second,
		size:          5,
		minIdle:       0,
		locked:        map[*T]time.Time{},
		unlocked:      map[*T]time.Time{},
	}

	for _, opt := range options {
		opt(p)
	}

	ticker := time.NewTicker(p.janitorSleep)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				p.mutex.Lock()
				defer p.mutex.Unlock()

				for o := range p.locked {
					p.expire(ctx, o)
				}
				for o := range p.unlocked {
					p.expire(ctx, o)
				}
				p.locked = map[*T]time.Time{}
				p.unlocked = map[*T]time.Time{}

				p.closed = true

				return
			case <-ticker.C:
				err := p.CleanUp(ctx)
				if err != nil {
					p.errLogger(ctx, err, "failed to clean up the pool")
				}
			}
		}
	}()

	err := p.keepMinIdle(ctx)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Pool[T]) Borrow(ctx context.Context) (*T, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return nil, fmt.Errorf("on borrow: %w", ErrPoolClosed)
	}

	for o := range p.unlocked {
		ok, err := p.validate(ctx, o)
		if err != nil {
			return nil, fmt.Errorf("on validating on borrow: %w", err)
		}
		if ok {
			delete(p.unlocked, o)
			p.locked[o] = time.Now()
			return o, nil
		}

		delete(p.unlocked, o)
		p.expire(ctx, o)
	}

	// if we reached the limit of the pool, wait for a new one to be released
	for !p.closed && p.objectCount() >= p.size {
		p.mutex.Unlock()
		err := p.cond.Wait(ctx)
		p.mutex.Lock()
		if err != nil {
			return nil, fmt.Errorf("on borrow while waiting: %w", err)
		}
		// check again, since it may have shutdown in the meantime
		if p.closed {
			return nil, fmt.Errorf("on borrow: %w", ErrPoolClosed)
		}
	}

	o, err := p.create(ctx)
	if err != nil {
		return nil, fmt.Errorf("on borrow: %w", err)
	}
	p.locked[o] = time.Now()
	return o, nil
}

func (p *Pool[T]) Return(ctx context.Context, o *T) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return
	}

	if o != nil {
		delete(p.locked, o)
		p.unlocked[o] = time.Now()
		p.cond.Broadcast()
	}
}

func (p *Pool[T]) CleanUp(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return nil
	}

	expired := false
	now := time.Now()
	for o, t := range p.unlocked {
		if now.Sub(t) > p.idleTimeout {
			delete(p.unlocked, o)
			p.expire(ctx, o)
			expired = true
		}
	}
	for o, t := range p.locked {
		if now.Sub(t) > p.borrowTimeout {
			delete(p.locked, o)
			p.expire(ctx, o)
			expired = true
		}
	}

	err := p.keepMinIdle(ctx)
	if err != nil {
		return fmt.Errorf("on cleanup: %w", err)
	}

	if expired {
		p.cond.Broadcast()
	}

	return nil
}

func (p *Pool[T]) keepMinIdle(ctx context.Context) error {
	idle := len(p.unlocked)
	if idle >= p.minIdle || p.objectCount() >= p.size {
		return nil
	}

	for i := idle; i < p.minIdle; i++ {
		o, err := p.create(ctx)
		if err != nil {
			return fmt.Errorf("on keeping the idle minimum: %w", err)
		}
		p.unlocked[o] = time.Now()

	}
	return nil
}

func (p *Pool[T]) objectCount() int {
	return len(p.unlocked) + len(p.locked)
}
