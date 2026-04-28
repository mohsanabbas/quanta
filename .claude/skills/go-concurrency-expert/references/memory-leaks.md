# Go Memory Leak Detection & Prevention

Hard rules for identifying, preventing, and fixing memory leaks in concurrent Go programs.

---

## The Three Categories of Go Memory Leaks

### Category 1: Goroutine Leaks (Most Common)

A goroutine that never returns leaks its entire stack (starts at 2KB, can grow dynamically) plus
every value it references. The GC cannot collect any of it because the goroutine is still
"alive" from the runtime's perspective.

**Common causes:**

#### 1a. Blocked channel read — no sender will ever write

```go
// LEAK: ch is never written to or closed
func leaky() {
    ch := make(chan string)
    go func() {
        s := <-ch          // blocks forever
        fmt.Println(s)
    }()
}
```

**Fix:** Ensure every channel read has a corresponding write, or use a done/context mechanism.

#### 1b. Blocked channel write — no consumer will ever read

```go
// LEAK: if consumer breaks early, goroutine blocks on write forever
func produce(done <-chan struct{}) <-chan int {
    ch := make(chan int)
    go func() {
        for i := 0; ; i++ {
            ch <- i   // blocks if nobody reads
        }
    }()
    return ch
}
```

**Fix:** Add a select with a done channel or context:

```go
go func() {
    defer close(ch)
    for i := 0; ; i++ {
        select {
        case ch <- i:
        case <-done:
            return
        }
    }
}()
```

#### 1c. for-range on a channel that never closes

```go
// LEAK: if producer forgets to close(ch), this goroutine hangs forever
go func() {
    for v := range ch {
        process(v)
    }
}()
```

**Fix:** The producer MUST close the channel when done writing.

#### 1d. Generator pattern without cancel

```go
// LEAK: if caller breaks early from the loop
func countTo(max int) <-chan int {
    ch := make(chan int)
    go func() {
        for i := 0; i < max; i++ {
            ch <- i // blocks if nobody reads
        }
        close(ch)
    }()
    return ch
}

for v := range countTo(1000) {
    if v > 5 { break } // goroutine leaks — it's stuck trying to write 6
}
```

**Fix:** Return a cancel function:

```go
func countTo(max int) (<-chan int, func()) {
    ch := make(chan int)
    done := make(chan struct{})
    cancel := func() { close(done) }
    go func() {
        defer close(ch)
        for i := 0; i < max; i++ {
            select {
            case ch <- i:
            case <-done:
                return
            }
        }
    }()
    return ch, cancel
}
```

---

### Category 2: Ticker & Timer Leaks

`time.NewTicker` creates an internal goroutine that fires at intervals. If you never call
`Stop()`, that goroutine lives forever.

```go
// LEAK: ticker goroutine never stops
func leakyTicker() {
    ticker := time.NewTicker(time.Second)
    done := make(chan bool)
    go func() {
        for {
            select {
            case <-ticker.C:
                fmt.Println("tick")
            case <-done:
                return
            }
        }
    }()
    // ... ticker.Stop() never called
}
```

**Fix:**

```go
func fixedTicker() {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()  // ALWAYS defer Stop immediately after creation
    // ...
}
```

**Rule:** `time.Timer` is safer (fires once), but still call `timer.Stop()` if you abandon it
before it fires, to release resources immediately.

---

### Category 3: Unbounded Data Structure Growth

Garbage collection handles unreferenced memory, but it cannot help when referenced data
structures grow without bound.

**Common causes:**

- Maps used as caches with no eviction policy (use an LRU cache instead)
- Slices that are appended to but never trimmed
- Global registries that accumulate entries without cleanup

**Rule:** Every data structure that grows must have a corresponding shrink mechanism. If it
doesn't shrink, set a maximum size and enforce it.

---

## Detection Tools

### 1. Race Detector

```bash
go run -race ./...
go test -race ./...
```

Not a leak detector, but catches data races that often accompany concurrency bugs that cause
leaks. Run it on every test suite. Zero tolerance for race warnings.

### 2. Goroutine Profiling

```go
import _ "net/http/pprof"

// In main:
go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
```

Then:

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

If goroutine count grows over time without bound, you have a leak. Look at the stack traces
to find which goroutines are stuck and where.

### 3. goleak (Uber's goroutine leak detector for tests)

```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

Fails the test if any goroutines are still running after the test completes (ignoring known
background goroutines). Use in every test suite that involves goroutines.

### 4. Runtime metrics

```go
fmt.Println("Goroutines:", runtime.NumGoroutine())
```

Log this periodically in production. A monotonically increasing goroutine count = leak.

### 5. Go 1.26 goroutineleak Profile (Production)

Go 1.26 adds an experimental `goroutineleak` profile to `runtime/pprof`. Unlike `goleak`
(test-only) and `runtime.NumGoroutine` (count without context), it uses the GC's reachability
analysis to identify goroutines permanently blocked on synchronization primitives that no
runnable goroutine can reach \u2014 exact leaks with stack traces, in production.

```go
import _ "net/http/pprof"

go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
```

Collect from a live process:

```bash
curl -s http://localhost:6060/debug/pprof/goroutineleak?debug=2
go tool pprof http://localhost:6060/debug/pprof/goroutineleak
```

Use this in addition to `goleak` in tests \u2014 they catch leaks at different stages.

### 6. testing/synctest (Go 1.24+)

`testing/synctest` lets you test concurrent code with fake time, so you can verify that
goroutines exit when their context cancels or their parent scope ends \u2014 without flaky
real-time `time.Sleep` calls.

```go
import "testing/synctest"

func TestWorkerCancels(t *testing.T) {
    synctest.Run(func() {
        ctx, cancel := context.WithCancel(context.Background())
        go worker(ctx)
        cancel()
        synctest.Wait() // blocks until all goroutines in the bubble are done or blocked
        // assert goroutine exited
    })
}
```

---

## Prevention Checklist

Before merging any PR with concurrent Go code:

- [ ] Every `go func()` has a documented, reachable exit path
- [ ] Every `go func()` uses a done channel, context, or reads from a channel that will close
- [ ] Every `time.NewTicker` has a `defer ticker.Stop()` on the next line
- [ ] Every `context.WithCancel/WithTimeout/WithDeadline` has a `defer cancel()` on the next line
- [ ] No goroutine writes to a channel without a `select` that includes a done/context case
- [ ] No generator function returns a channel without also returning a cancel function
- [ ] `go test -race ./...` passes with zero warnings
- [ ] Goroutine count in tests (via goleak or runtime.NumGoroutine) is stable, not growing
- [ ] Maps used as caches have an eviction policy or maximum size
- [ ] Buffered channels are justified with a documented reason for the buffer size
