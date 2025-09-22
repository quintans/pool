# Pool

A thread-safe, generic object pool implementation in Go with automatic lifecycle management, configurable timeouts, and context-aware operations.

## Features

- **Generic**: Works with any type using Go generics
- **Thread-safe**: Concurrent access with proper synchronization
- **Context-aware**: Respects context cancellation and timeouts
- **Automatic cleanup**: Background janitor removes expired objects
- **Configurable**: Flexible options for pool size, timeouts, and validation
- **Resource-safe**: Proper cleanup on shutdown and error conditions

## Installation

```bash
go get github.com/quintans/pool
```

## Quick Start

```go
package main

import (
    "context"
    "time"
    
    "github.com/pkg/sftp"
    "golang.org/x/crypto/ssh"
    "github.com/quintans/pool"
)

func main() {
    ctx := context.Background()
    
    // Create a pool of SFTP connections
    sftpPool, err := pool.New[sftp.Client](
        ctx,
        // Create function
        func(ctx context.Context) (*sftp.Client, error) {
            sshClient, err := ssh.Dial("tcp", "example.com:22", &ssh.ClientConfig{
                User: "username",
                Auth: []ssh.AuthMethod{ssh.Password("password")},
                HostKeyCallback: ssh.InsecureIgnoreHostKey(),
            })
            if err != nil {
                return nil, err
            }
            return sftp.NewClient(sshClient)
        },
        // Cleanup function
        func(ctx context.Context, client *sftp.Client) {
            client.Close()
        },
        // Options
        pool.Size[sftp.Client](10),                    // Max 10 connections
        pool.MinIdle[sftp.Client](2),                  // Keep 2 idle connections
        pool.IdleTimeout[sftp.Client](5*time.Minute), // Expire after 5 min idle
    )
    if err != nil {
        panic(err)
    }
    
    // Borrow a connection
    client, err := sftpPool.Borrow(ctx)
    if err != nil {
        panic(err)
    }
    
    // Use the SFTP connection
    files, err := client.ReadDir("/remote/path")
    // ... handle file operations
    
    // Return the connection to the pool
    sftpPool.Return(ctx, client)
}
```

## Configuration Options

### Pool Size and Capacity

```go
pool.Size[T](10)        // Maximum pool size (default: 5)
pool.MinIdle[T](2)      // Minimum idle objects to maintain (default: 0)
```

### Timeout Configuration

```go
pool.IdleTimeout[T](5*time.Minute)    // Expire idle objects after 5 minutes
pool.BorrowTimeout[T](30*time.Second) // Expire borrowed objects after 30 seconds
pool.JanitorSleep[T](1*time.Minute)   // Janitor cleanup interval
```

### Object Validation

```go
pool.Validate(func(ctx context.Context, obj *MyType) (bool, error) {
    // Return true if object is still valid, false to discard
    return obj.IsConnected(), nil
})
```

### Error Logging

```go
pool.ErrLogger(func(ctx context.Context, err error, msg string) {
    log.Printf("Pool error: %s - %v", msg, err)
})
```

## Advanced Usage

### HTTP Client Pool

```go
httpPool, err := pool.New[http.Client](
    ctx,
    func(ctx context.Context) (*http.Client, error) {
        return &http.Client{
            Timeout: 10 * time.Second,
        }, nil
    },
    func(ctx context.Context, client *http.Client) {
        client.CloseIdleConnections()
    },
    pool.Size[http.Client](20),
    pool.IdleTimeout[http.Client](2*time.Minute),
)
```

### Redis Connection Pool with Validation

```go
redisPool, err := pool.New[redis.Conn](
    ctx,
    func(ctx context.Context) (*redis.Conn, error) {
        return redis.Dial("tcp", "localhost:6379")
    },
    func(ctx context.Context, conn *redis.Conn) {
        conn.Close()
    },
    pool.Size[redis.Conn](15),
    pool.Validate(func(ctx context.Context, conn *redis.Conn) (bool, error) {
        _, err := conn.Do("PING")
        return err == nil, err
    }),
)
```

## API Reference

### Creating a Pool

```go
func New[T any](
    ctx context.Context,
    create func(context.Context) (*T, error),
    expire func(context.Context, *T),
    options ...Option[T],
) (*Pool[T], error)
```

### Pool Operations

```go
// Borrow an object from the pool (blocks if pool is full)
func (p *Pool[T]) Borrow(ctx context.Context) (*T, error)

// Return an object to the pool
func (p *Pool[T]) Return(ctx context.Context, o *T)

// Manually trigger cleanup (automatically runs via janitor)
func (p *Pool[T]) CleanUp(ctx context.Context) error
```

### Error Handling

The pool returns `ErrPoolClosed` when operations are attempted on a closed pool:

```go
obj, err := pool.Borrow(ctx)
if errors.Is(err, pool.ErrPoolClosed) {
    // Pool has been shut down
}
```

## Lifecycle Management

### Object States

1. **Created**: New objects created via the `create` function
2. **Borrowed**: Objects currently in use by clients (tracked in `locked` map)
3. **Returned**: Objects available for reuse (tracked in `unlocked` map)  
4. **Expired**: Objects cleaned up via the `expire` function

### Automatic Cleanup

The pool runs a background janitor that:
- Removes idle objects after `IdleTimeout`
- Removes borrowed objects after `BorrowTimeout` 
- Maintains `MinIdle` objects when possible
- Respects the maximum `Size` limit

### Graceful Shutdown

When the context is cancelled, the pool automatically:
- Stops accepting new borrows
- Expires all objects (borrowed and idle)
- Cleans up resources via the `expire` function

## Thread Safety

All pool operations are thread-safe and can be called concurrently from multiple goroutines. The pool uses:
- Mutex protection for all state changes
- Custom condition variable for context-aware blocking
- Atomic operations where appropriate

## Performance Considerations

- Object validation only occurs when borrowing from the idle pool
- Janitor cleanup is time-based to avoid excessive overhead
- Pool blocking allows backpressure instead of unbounded growth
- Zero allocations for successful borrow/return cycles

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
