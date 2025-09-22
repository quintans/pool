package pool_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quintans/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Foo struct {
	name string
}

func TestBorrowValidate(t *testing.T) {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	valid := false
	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) { return &Foo{"foo"}, nil },
		func(ctx context.Context, f *Foo) {
			f.name = ""
		},
		pool.Validate(func(ctx context.Context, t *Foo) (bool, error) {
			valid = true
			return true, nil
		}),
	)
	require.NoError(t, err)

	f, err := p.Borrow(ctx)
	require.NoError(t, err)
	assert.False(t, valid)
	assert.Equal(t, "foo", f.name)

	p.Return(ctx, f)

	f, err = p.Borrow(ctx)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestIdleTimeout(t *testing.T) {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) { return &Foo{"foo"}, nil },
		func(ctx context.Context, f *Foo) {
			f.name = ""
			wg.Done()
		},
		pool.IdleTimeout[Foo](500*time.Millisecond),
		pool.JanitorSleep[Foo](500*time.Millisecond),
	)
	require.NoError(t, err)

	f, err := p.Borrow(ctx)
	require.NoError(t, err)

	p.Return(ctx, f)

	wg.Wait()
	assert.Equal(t, "", f.name)
}

func TestBorrowBlockWithTimeout(t *testing.T) {
	ctx := context.Background()

	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) { return &Foo{"foo"}, nil },
		func(ctx context.Context, f *Foo) {},
		pool.Size[Foo](2),
	)
	require.NoError(t, err)

	// exhaust pool
	for range 2 {
		_, err := p.Borrow(ctx)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	// should block
	_, err = p.Borrow(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGiven_BlockedBorrow_when_Return_then_BorrowShouldUnblock(t *testing.T) {
	ctx := context.Background()

	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) { return &Foo{"foo"}, nil },
		func(ctx context.Context, f *Foo) {},
		pool.Size[Foo](1),
	)
	require.NoError(t, err)

	// exhaust pool
	foo, err := p.Borrow(ctx)
	require.NoError(t, err)

	go func() {
		p.Return(ctx, foo)
	}()

	_, err = p.Borrow(ctx)

	require.NoError(t, err)
}

func TestBorrowTimeout(t *testing.T) {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) { return &Foo{"foo"}, nil },
		func(ctx context.Context, f *Foo) {
			f.name = ""
			wg.Done()
		},
		pool.BorrowTimeout[Foo](500*time.Millisecond),
		pool.JanitorSleep[Foo](500*time.Millisecond),
	)
	require.NoError(t, err)

	f, err := p.Borrow(ctx)
	require.NoError(t, err)

	wg.Wait()
	assert.Equal(t, "", f.name)
}

func TestMinIdle(t *testing.T) {
	ctx := context.Background()

	var counter atomic.Int32
	_, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) {
			counter.Add(1)
			return &Foo{"foo"}, nil
		},
		func(ctx context.Context, f *Foo) {},
		pool.MinIdle[Foo](3),
	)
	require.NoError(t, err)
	require.Equal(t, int32(3), counter.Load())
}

func TestCancelContext(t *testing.T) {
	ctx := context.Background()

	var count atomic.Int32
	p, err := pool.New(
		ctx,
		func(ctx context.Context) (*Foo, error) {
			count.Add(1)
			return &Foo{"foo"}, nil
		},
		func(ctx context.Context, f *Foo) {},
	)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	_, err = p.Borrow(ctx)
	require.ErrorIs(t, err, pool.ErrPoolClosed)
}
