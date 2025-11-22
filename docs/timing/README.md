# Timing Utilities

## Overview

The `timing` package provides simple utilities for running periodic tasks with automatic cancellation and error handling. It wraps Go's `time.Ticker` with context cancellation and structured logging.

## Why This Package?

### The Repetitive Pattern

**Common periodic task pattern**:
```go
ticker := time.NewTicker(5 * time.Second)
go func() {
    for {
        select {
        case <-ticker.C:
            if err := doSomething(); err != nil {
                log.Error().Err(err).Msg("task failed")
            }
        case <-stopChan:
            ticker.Stop()
            return
        }
    }
}()
```

**Repeated everywhere**: Auth checks, health monitoring, stats collection

**Issues**:
- Boilerplate repeated
- Easy to forget `ticker.Stop()`
- No standardized error logging
- Manual goroutine management

### The Solution

**Simplified periodic tasks**:
```go
cancel := timing.RunOnTicker(log, 5*time.Second, func() error {
    return doSomething()
})

// Later: stop the task
cancel()
```

**Benefits**:
- ✓ One-liner setup
- ✓ Automatic cleanup
- ✓ Consistent error logging
- ✓ Context cancellation support

## Usage

### Basic Periodic Task

```go
import "plane.watch/lib/timing"

// Run every 5 seconds until cancelled
cancel := timing.RunOnTicker(log, 5*time.Second, func() error {
    // Your periodic task
    fmt.Println("Task running...")
    return nil
})

// Stop after 30 seconds
time.Sleep(30 * time.Second)
cancel()
```

**What happens**:
1. Ticker created with 5-second interval
2. Goroutine spawned
3. Function called every 5 seconds
4. Errors logged automatically
5. Stops when `cancel()` called

### With Context

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
defer cancel()

timing.RunOnTickerWithContext(ctx, log, 5*time.Second, func() error {
    // Task code
    return performCheck()
})

// Task stops when context cancelled (after 1 minute)
```

**Why with context**: Integration with existing cancellation
- Parent context cancels → task stops
- Timeout contexts work naturally
- Request-scoped tasks

## Error Handling

### Automatic Logging

**Errors don't stop ticker**:
```go
timing.RunOnTicker(log, time.Second, func() error {
    if rand.Intn(2) == 0 {
        return errors.New("random failure")
    }
    return nil
})
```

**Behavior**:
- Error returned → Logged at ERROR level
- Ticker continues running
- Next tick tries again

**Log output**:
```
ERROR function returned error error="random failure" component="timing.RunOnTicker" interval="1s"
```

**Why continue on error**: Transient failures shouldn't stop monitoring
- Network blip → Retry next interval
- Temporary resource issue → May resolve
- Partial failure → Other tasks still work

### Stopping on Error

**If you want to stop on error**:
```go
cancel := timing.RunOnTicker(log, time.Second, func() error {
    if criticalError := checkCritical(); criticalError != nil {
        cancel()  // Stop ticker
        return criticalError
    }
    return nil
})
```

**Closure captures cancel**: Can call from within function

## Use Cases in Codebase

### Periodic Authentication Check

**From mlatbridge**:
```go
eg.Go(func() error {
    timing.RunOnTickerWithContext(connCtx, mb.log, time.Second*5, func() error {
        if !mb.feeders.IsValid(apiKey) {
            connCtxCancel()  // Poison pill
        }
        return nil
    })
    return nil
})
```

**Every 5 seconds**: Check if feeder still authorized
- Auth revoked → Cancel connection context
- Connection closed gracefully

### Poison Pill Pattern

**From producer**:
```go
if p.poisonPill != nil {
    p.poisonPillCancel = timing.RunOnTicker(p.log, time.Second*5, func() error {
        if p.poisonPill() {
            log.Debug().Msg("took poison pill")
            p.Stop()
        }
        return nil
    })
}
```

**Periodic condition check**: Stop when condition met

### Stats Collection

```go
timing.RunOnTicker(log, time.Minute, func() error {
    stats := collectStats()
    publishStats(stats)
    return nil
})
```

**Every minute**: Gather and publish metrics

### Cache Cleanup

```go
timing.RunOnTicker(log, 5*time.Minute, func() error {
    count := cache.EvictExpired()
    log.Debug().Int("evicted", count).Msg("cache cleanup")
    return nil
})
```

**Every 5 minutes**: Remove stale cache entries

## Logging Details

### Structured Context

**Automatic fields added**:
```go
logger = logger.With().
    Str("component", "timing.RunOnTicker").
    Str("interval", t.String()).
    Logger()
```

**Every log includes**:
- `component`: "timing.RunOnTicker"
- `interval`: "5s", "1m", etc.

**Error logs also include**: Original error

**Example output**:
```
ERROR function returned error
  component="timing.RunOnTicker"
  interval="30s"
  error="connection refused"
```

**Why structured**: Easy filtering
```bash
# Find all timing errors
grep "timing.RunOnTicker" logs | grep ERROR

# Find specific interval failures
grep 'interval="30s"' logs
```

## Implementation Details

### Goroutine Lifecycle

**Spawned immediately**:
```go
go func() {
    for {
        select {
        case <-ticker.C:
            f()
        case <-ctx.Done():
            ticker.Stop()
            return  // Goroutine exits
        }
    }
}()
```

**Exits when**: Context cancelled

**Ticker cleanup**: `ticker.Stop()` called before exit
- Releases timer resources
- Prevents goroutine leak
- No channel leak

### First Execution

**Not immediate**: Waits for first tick
```go
timing.RunOnTicker(log, 5*time.Second, f)
// f() called after 5 seconds, not immediately
```

**If you need immediate execution**:
```go
f()  // Call once immediately
cancel := timing.RunOnTicker(log, 5*time.Second, f)  // Then periodic
```

**Or use pattern**:
```go
timing.RunOnTicker(log, 5*time.Second, func() error {
    // This runs after first interval
    return f()
})
f()  // This runs immediately
```

### Cancel Function

**Returns `context.CancelFunc`**:
```go
type CancelFunc func()
```

**Can be called multiple times**: Safe, idempotent
```go
cancel()
cancel()  // No-op, already cancelled
```

**No return value**: Fire-and-forget

**Non-blocking**: Returns immediately

## Performance Characteristics

### Memory

**Per ticker**:
- Goroutine: ~2 KB stack
- Ticker: ~100 bytes
- Context: ~50 bytes
- **Total**: ~2-3 KB per ticker

**1000 tickers**: ~2-3 MB (negligible)

### CPU

**Idle CPU**: Near zero
- Ticker sleeps between intervals
- No busy-waiting
- Efficient OS-level timers

**During execution**: Depends on function `f()`
- Ticker overhead: <1 µs
- Function execution: Variable

### Goroutine Count

**One per ticker**:
```go
// 3 tickers = 3 goroutines
cancel1 := timing.RunOnTicker(...)
cancel2 := timing.RunOnTicker(...)
cancel3 := timing.RunOnTicker(...)
```

**Not a problem**: Go handles thousands of goroutines easily

**Leak prevention**: Always call `cancel()` when done
```go
defer cancel()
```

## Common Patterns

### Bounded Execution

**Stop after N iterations**:
```go
count := 0
cancel := timing.RunOnTicker(log, time.Second, func() error {
    count++
    if count >= 10 {
        cancel()  // Stop after 10 iterations
    }
    return doWork()
})
```

### Adaptive Interval

**Current**: Fixed interval

**Pattern for variable interval**:
```go
var interval = time.Second
var cancel context.CancelFunc

updateInterval := func(newInterval time.Duration) {
    if cancel != nil {
        cancel()  // Stop old ticker
    }
    interval = newInterval
    cancel = timing.RunOnTicker(log, interval, task)
}
```

### Synchronized Start

**Multiple tickers starting together**:
```go
var cancels []context.CancelFunc

// Start all at once
for i := 0; i < 10; i++ {
    cancel := timing.RunOnTicker(log, time.Second, tasks[i])
    cancels = append(cancels, cancel)
}

// Stop all at once
for _, cancel := range cancels {
    cancel()
}
```

## Comparison to Alternatives

### time.Ticker directly

**Raw ticker**:
```go
ticker := time.NewTicker(5 * time.Second)
go func() {
    for range ticker.C {
        doSomething()
    }
}()
```

**Cons**:
- No cancellation
- Goroutine leak (runs forever)
- No error handling
- No logging

**When to use direct**: Very simple cases, manual control

### time.AfterFunc

**One-shot timer**:
```go
time.AfterFunc(5 * time.Second, func() {
    doSomething()  // Called once after 5s
})
```

**Not periodic**: Single execution only

**Use case**: Delayed execution, not periodic tasks

### Cron-like solutions

**github.com/robfig/cron**:
```go
c := cron.New()
c.AddFunc("@every 1m", func() {
    doSomething()
})
c.Start()
```

**Overkill for simple intervals**: Cron syntax, more complex

**Use when**: Need cron expressions ("0 0 * * *")

**timing package**: Simpler for interval-based tasks

## Common Issues

### Goroutine Not Stopping

**Symptom**: Goroutine count grows

**Cause**: Never calling `cancel()`
```go
timing.RunOnTicker(log, time.Second, f)
// Forgot to store cancel function!
// Goroutine runs forever
```

**Solution**: Always store and call cancel
```go
cancel := timing.RunOnTicker(log, time.Second, f)
defer cancel()
```

### First Run Delayed

**Symptom**: Task doesn't run immediately

**Expected behavior**: Waits for first tick

**Solution**: Call function before creating ticker
```go
f()  // Immediate execution
cancel := timing.RunOnTicker(log, interval, f)  // Periodic
```

### Errors Ignored

**Symptom**: Function returning errors but nothing happens

**Expected**: Errors logged, execution continues

**Check logs**: Look for "function returned error"

**If you need to act on errors**: Check error in function, call `cancel()` if critical

## Best Practices

### Always Defer Cancel

```go
func setupMonitoring() {
    cancel := timing.RunOnTicker(log, time.Minute, collectMetrics)
    defer cancel()  // Ensures cleanup
}
```

### Use Descriptive Loggers

```go
logger := log.With().Str("task", "health-check").Logger()
cancel := timing.RunOnTicker(logger, 30*time.Second, checkHealth)
```

**Benefit**: Errors clearly identified
```
ERROR function returned error task="health-check" ...
```

### Handle Panics

**Ticker doesn't recover panics**:
```go
timing.RunOnTicker(log, time.Second, func() error {
    defer func() {
        if r := recover(); r != nil {
            log.Error().Interface("panic", r).Msg("task panicked")
        }
    }()

    riskyOperation()
    return nil
})
```

### Keep Functions Fast

**Long-running tasks block next tick**:
```go
// BAD: 10-second task, 5-second interval
timing.RunOnTicker(log, 5*time.Second, func() error {
    time.Sleep(10 * time.Second)  // Blocks for 10s
    return nil
})
// Effective interval: 10s, not 5s
```

**Solution**: Ensure task < interval
```go
timing.RunOnTicker(log, 15*time.Second, func() error {
    time.Sleep(10 * time.Second)  // Finishes before next tick
    return nil
})
```

## Future Enhancements

<!--
Maintainers: Document enhancements as implemented
-->

### Metrics Integration

**Proposed**: Track task execution
```go
type Metrics struct {
    Executions prometheus.Counter
    Errors     prometheus.Counter
    Duration   prometheus.Histogram
}

RunOnTickerWithMetrics(log, interval, f, metrics)
```

### Immediate First Execution Option

**Proposed**:
```go
RunOnTickerImmediate(log, interval, f)  // Calls f() immediately, then periodically
```

### Jitter Support

**Proposed**: Randomize interval slightly
```go
RunOnTickerWithJitter(log, interval, jitter, f)
// interval=5s, jitter=1s → runs every 4-6s randomly
```

**Use case**: Prevent thundering herd

## File Guide

| File | Purpose |
|------|---------|
| `ticker.go` | Periodic task execution with cancellation |

## See Also

- `time.Ticker` - Underlying Go ticker
- `context.Context` - Cancellation mechanism
- [Producer](../producer/README.md) - Uses timing for poison pill

## References

- Go time package: https://pkg.go.dev/time
- Context cancellation: https://go.dev/blog/context
