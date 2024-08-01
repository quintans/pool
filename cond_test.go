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

func TestWaitSuccessful(t *testing.T) {
	cond := pool.NewCond()

	go func() {
		time.Sleep(100 * time.Millisecond)
		cond.Broadcast()
	}()

	err := cond.Wait(context.Background())
	require.NoError(t, err)
}

func TestWaitTimeout(t *testing.T) {
	cond := pool.NewCond()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()

	err := cond.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMultipleWait(t *testing.T) {
	cond := pool.NewCond()

	count := atomic.Int32{}

	var wg2 sync.WaitGroup
	wg2.Add(1)
	go func() {
		err := cond.Wait(context.Background())
		require.NoError(t, err)
		count.Add(1)
		wg2.Done()
	}()

	wg2.Add(1)
	go func() {
		err := cond.Wait(context.Background())
		require.NoError(t, err)
		count.Add(1)
		wg2.Done()
	}()

	time.Sleep(100 * time.Millisecond)
	cond.Broadcast()

	wg2.Wait()

	assert.Equal(t, int32(2), count.Load())
}
