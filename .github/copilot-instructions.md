# Pool Library - AI Coding Guidelines

## Architecture Overview

This is a generic object pool library in Go that manages the lifecycle of expensive-to-create objects (database connections, HTTP clients, etc.). The core architecture consists of:

- **`Pool[T]`**: Main pool structure with thread-safe object management using `locked` and `unlocked` maps to track object states and timestamps
- **`Cond`**: Custom condition variable implementation using channels instead of `sync.Cond` for context-aware waiting
- **Janitor goroutine**: Background cleanup process that expires idle/borrowed objects based on configurable timeouts

## Key Design Patterns

### Generic Object Pool Pattern
```go
// All pool operations follow this create → borrow → return → expire lifecycle
pool.New[MyType](ctx, createFunc, expireFunc, options...)
obj, err := pool.Borrow(ctx)  // Blocks if pool is full, respects context cancellation
pool.Return(ctx, obj)         // Makes object available for reuse
```

### Functional Options Pattern
All pool configuration uses functional options (`Option[T]`) - when adding new configuration, follow this pattern:
```go
func NewOption[T any](value SomeType) Option[T] {
    return func(p *Pool[T]) {
        p.field = value
    }
}
```

### Context-Aware Operations
Every public method accepts `context.Context` as first parameter. The pool respects context cancellation for:
- Blocking borrows when pool is exhausted
- Graceful shutdown via context cancellation
- Timeout enforcement (borrow timeout, validation timeouts)

## Critical Implementation Details

### Thread Safety
- All pool state changes are protected by `Pool.mutex`
- The custom `Cond` type enables context-aware waiting without the limitations of `sync.Cond`
- Never hold locks across user-provided callbacks (`create`, `expire`, `validate`)

### Object Lifecycle Management
- `locked` map: Objects currently borrowed by clients (key=object, value=borrow timestamp)
- `unlocked` map: Available objects waiting for reuse (key=object, value=return timestamp)
- Objects transition: `unlocked` → `locked` (on borrow) → `unlocked` (on return) → expired (via janitor)

### Testing Patterns
- Use `sync/atomic` for concurrent counters in tests (see `TestMinIdle`)
- Test blocking scenarios with `sync.WaitGroup` coordination (see `TestBorrowBlockWithTimeout`)
- Always test both success and timeout paths for context-dependent operations
- Mock objects should implement cleanup verification (see how `Foo.name` is cleared in tests)

## Development Guidelines

### Error Handling
- Wrap errors with context using `fmt.Errorf("on operation: %w", err)`
- Use `ErrPoolClosed` for operations on closed pools
- Validate user inputs in option functions (e.g., `Size` ensures minimum value of 1)

### Performance Considerations
- Object validation only occurs on borrow from `unlocked` pool, not on fresh creation
- Janitor cleanup is time-based, not event-driven, to avoid excessive cleanup cycles
- Pool size limits prevent unbounded growth but allow blocking until resources are available

### Adding New Features
- New timeouts/limits should be configurable via functional options
- Background processes should respect the main context for graceful shutdown
- State changes should trigger `cond.Broadcast()` to wake blocked borrowers

## Common Workflows

**Running Tests**: `go test -v ./...`  
**Build**: `go build ./...`  
**Benchmarks**: Add benchmark tests following Go conventions in `*_test.go` files

This codebase prioritizes correctness and resource safety over raw performance, making it suitable for production use with expensive resources.