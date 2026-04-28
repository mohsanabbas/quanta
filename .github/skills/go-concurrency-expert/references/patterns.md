# Go Concurrency Patterns Reference

Complete pattern implementations with rules for when to use each.

---

## 1. Done Channel Pattern

**Use when:** You need to signal one or more goroutines to stop. The foundational cancellation
pattern in Go.

**Rule:** Use `chan struct{}` for the done channel. Never write to it — only close it. A close
is broadcast to all readers.

```go
func searchData(s string, searchers []func(string) []string) []string {
    done := make(chan struct{})
    result := make(chan []string)
    for _, searcher := range searchers {
        go func(searcher func(string) []string) {
            select {
            case result <- searcher(s):
            case <-done:
            }
        }(searcher)
    }
    r := <-result
    close(done) // signals all other goroutines to exit
    return r
}
```

---

## 2. Cancel Function Pattern

**Use when:** You return a channel from a function and need to give the caller a way to stop
the goroutine producing values.

**Rule:** Always return the cancel function alongside the channel. The caller MUST call it,
even if they consume all values.

```go
func countTo(max int) (<-chan int, func()) {
    ch := make(chan int)
    done := make(chan struct{})
    cancel := func() { close(done) }
    go func() {
        for i := 0; i < max; i++ {
            select {
            case <-done:
                return
            case ch <- i:
            }
        }
        close(ch)
    }()
    return ch, cancel
}

// Usage:
ch, cancel := countTo(100)
defer cancel() // MUST call even if you read all values
for v := range ch {
    if v > 5 { break }
    fmt.Println(v)
}
```

---

## 3. Fan-In (Funnel) Pattern

**Use when:** You have N producers generating output on separate channels and need to merge
them into a single stream for unified processing.

**Participants:**

- **Sources**: N input channels of the same type
- **Destination**: Single output channel (created by Funnel)
- **Funnel**: Function that wires sources to destination

**Rule:** Use `sync.WaitGroup` to close the destination channel only after ALL source channels
are closed and their goroutines have exited.

```go
func Funnel(sources ...<-chan int) <-chan int {
    dest := make(chan int)
    var wg sync.WaitGroup
    wg.Add(len(sources))
    for _, ch := range sources {
        go func(c <-chan int) {
            defer wg.Done()
            for n := range c {
                dest <- n
            }
        }(ch)
    }
    go func() {
        wg.Wait()
        close(dest)
    }()
    return dest
}
```

---

## 4. Fan-Out (Split) Pattern

**Use when:** You need to parallelize a workload by distributing input from one channel across
N worker goroutines.

**Participants:**

- **Source**: Single input channel
- **Destinations**: N output channels (created by Split)
- **Split**: Function that creates workers competing for reads

**Implementation choice:**

- Round-robin (single goroutine distributes): simpler but a slow consumer stalls everything
- Competing readers (goroutine per destination): slightly more resources but no single-consumer
  bottleneck — **prefer this approach**

```go
func Split(source <-chan int, n int) []<-chan int {
    dests := make([]<-chan int, 0, n)
    for i := 0; i < n; i++ {
        ch := make(chan int)
        dests = append(dests, ch)
        go func() {
            defer close(ch)
            for val := range source {
                ch <- val
            }
        }()
    }
    return dests
}
```

**Rule:** When source closes, all destination goroutines terminate and close their channels.
The caller should use `sync.WaitGroup` to wait for all destination consumers.

---

## 5. Future Pattern

**Use when:** You want to start an async computation and provide the caller with a clean,
synchronous-looking API to retrieve the result. Channels-as-futures work but become awkward
when multiple goroutines need the same result.

**Participants:**

- **Future**: Interface the consumer calls to get results (blocks until ready)
- **SlowFunction**: Wrapper that launches the goroutine and returns the Future
- **InnerFuture**: Implements Future; caches results after first read

**Rule:** Use a single struct result channel — never parallel `resCh`/`errCh` channels (it's
easy to deadlock if one is read but not the other). Close the channel from the producer; use
`sync.Once` so subsequent `Result()` calls return the cached value safely.

```go
type Future interface {
    Result() (string, error)
}

type result struct {
    val string
    err error
}

type innerFuture struct {
    once sync.Once
    res  result
    ch   <-chan result
}

func (f *innerFuture) Result() (string, error) {
    f.once.Do(func() { f.res = <-f.ch })
    return f.res.val, f.res.err
}

func SlowFunction(ctx context.Context) Future {
    ch := make(chan result, 1) // buffered so producer can exit even if Result() never called
    go func() {
        select {
        case <-time.After(2 * time.Second):
            ch <- result{val: "done"}
        case <-ctx.Done():
            ch <- result{err: ctx.Err()}
        }
    }()
    return &innerFuture{ch: ch}
}
```

Note the buffered channel of size 1: if the consumer abandons the future, the producer
goroutine still completes its single send and exits — preventing a leak.

---

## 6. Worker Pool Pattern

**Use when:** You have a stream of jobs and want to limit the number of concurrent workers
processing them. Classic for HTTP request handlers, batch processors, and queue consumers.

```go
func workerPool(jobs <-chan Job, results chan<- Result, numWorkers int) {
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for job := range jobs {
                results <- process(job)
            }
        }()
    }
    go func() {
        wg.Wait()
        close(results)
    }()
}
```

**Rules:**

- Buffer `results` to `numWorkers` so workers can exit immediately after writing
- Close `jobs` from the producer side to signal no more work
- Use WaitGroup + monitoring goroutine to close `results` after all workers exit

---

## 7. Pipeline Pattern

**Use when:** Data flows through a series of stages, each performing a transformation. Each
stage is a goroutine connected by channels.

```go
func stage1(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for v := range in {
            out <- v * 2
        }
    }()
    return out
}

// Chain: source -> stage1 -> stage2 -> consumer
output := stage2(stage1(source))
```

**Rule:** Each stage closes its output channel when its input channel is exhausted. This
propagates shutdown through the entire pipeline.

---

## 8. Backpressure Pattern

**Use when:** You need to reject excess work rather than queue it unboundedly. Essential for
services that must maintain responsiveness under load.

```go
type PressureGauge struct {
    ch chan struct{}
}

func NewPressureGauge(limit int) *PressureGauge {
    ch := make(chan struct{}, limit)
    for i := 0; i < limit; i++ {
        ch <- struct{}{}
    }
    return &PressureGauge{ch: ch}
}

func (pg *PressureGauge) Process(f func()) error {
    select {
    case <-pg.ch:       // acquire token
        f()
        pg.ch <- struct{}{} // return token
        return nil
    default:
        return errors.New("no capacity")
    }
}
```

**Rule:** The `default` case is correct HERE (standalone select, not for-select). It implements
non-blocking rejection. The buffered channel acts as a token bucket.

---

## 9. Sharded Map Pattern

**Use when:** A shared map protected by a single mutex becomes a bottleneck due to lock
contention under high concurrency. Sharding partitions the map into individually-lockable
segments.

```go
import "hash/fnv"

type Shard struct {
    sync.RWMutex
    m map[string]any
}

type ShardedMap []*Shard

func NewShardedMap(nshards int) ShardedMap {
    shards := make([]*Shard, nshards)
    for i := range shards {
        shards[i] = &Shard{m: make(map[string]any)}
    }
    return shards
}

func (m ShardedMap) getShard(key string) *Shard {
    h := fnv.New32a()
    h.Write([]byte(key))
    return m[h.Sum32()%uint32(len(m))]
}

func (m ShardedMap) Get(key string) interface{} {
    shard := m.getShard(key)
    shard.RLock()
    defer shard.RUnlock()
    return shard.m[key]
}

func (m ShardedMap) Set(key string, value interface{}) {
    shard := m.getShard(key)
    shard.Lock()
    defer shard.Unlock()
    shard.m[key] = value
}
```

**Rule:** When you need to lock ALL shards (e.g., for a Keys() method), do it concurrently with
a WaitGroup — never sequentially, or you'll serialize the very contention you're trying to avoid.

---

## 10. Monitor Goroutine Pattern

**Use when:** You want to share state via communicating, not communicate by sharing. A single
goroutine owns the state; others interact via channels.

**Rule:** Every monitor MUST have an exit path (Rule 1 from SKILL.md). Use a context or done
channel — a monitor with `for { select { ... } }` and no exit case is a guaranteed leak.

```go
type counter struct {
    reads  chan int
    writes chan int
}

func newCounter(ctx context.Context) *counter {
    c := &counter{
        reads:  make(chan int),
        writes: make(chan int),
    }
    go c.monitor(ctx)
    return c
}

func (c *counter) monitor(ctx context.Context) {
    var value int
    for {
        select {
        case <-ctx.Done():
            return // explicit exit path
        case v := <-c.writes:
            value = v
        case c.reads <- value:
        }
    }
}

func (c *counter) Set(v int) { c.writes <- v }
func (c *counter) Get() int  { return <-c.reads }
```

All traffic flows through the monitor's select block. There is exactly one instance of the
monitor goroutine. This makes race conditions structurally impossible — no mutex needed.
Note the API hides the channels (Rule 2).

---

## 11. Timeout Pattern

**Use when:** An operation must complete within a deadline or be abandoned.

```go
func withTimeout() (int, error) {
    var result int
    var err error
    done := make(chan struct{})
    go func() {
        result, err = doWork()
        close(done)
    }()
    select {
    case <-done:
        return result, err
    case <-time.After(2 * time.Second):
        return 0, errors.New("timeout")
    }
}
```

**Rule:** If a context is available, use `context.WithTimeout` instead of `time.After` — it
respects parent deadlines and is cancellable. The goroutine continues running after timeout;
use context cancellation to actually stop the work.

---

## 12. Ordered Execution with Signal Channels

**Use when:** You need goroutines to execute in a specific sequence (rare, but useful for
initialization chains).

```go
func A(trigger, next chan struct{}) {
    <-trigger
    fmt.Println("A")
    close(next)
}

func B(trigger, next chan struct{}) {
    <-trigger
    fmt.Println("B")
    close(next)
}

func C(trigger chan struct{}) {
    <-trigger
    fmt.Println("C")
}

func main() {
    ab := make(chan struct{})
    bc := make(chan struct{})
    start := make(chan struct{})

    go C(bc)
    go B(ab, bc)
    go A(start, ab)

    close(start) // triggers A -> B -> C
}
```

**Rule:** Each goroutine blocks on its trigger channel and closes its next channel when done.
This creates a chain. Only practical for small, fixed numbers of goroutines. For dynamic
ordering, use a pipeline (Pattern 7) instead.

---

## 13. The GatherAndProcess Pattern (Combining Multiple Patterns)

**Use when:** You need to call multiple services concurrently, combine partial results, and
enforce a global timeout. This is the canonical "real-world" Go concurrency pattern.

See the `processor` struct pattern from Cloud Native Go:

- Buffered channels for each goroutine's output (buffer = 1, so goroutines can exit after writing)
- Error channel buffered to the number of goroutines that can fail
- Context with timeout for global deadline
- `for-select` loop in waitForAB to collect partial results
- Each select case handles: success, error, and context cancellation

**Rule:** Use as little concurrency as your program needs to be correct. If you trust a called
function to respect context, call it directly instead of wrapping it in a goroutine.

---

## 14. Structured Concurrency with `errgroup` (Go 1.20+, idiomatic in 1.26+)

**Use when:** You need to fan out N concurrent operations, cancel siblings if any one fails,
and propagate the first error back to the caller. This is Go's answer to Python's `TaskGroup`
and Kotlin's `coroutineScope`.

**Rule:** Prefer `errgroup` over hand-rolled `sync.WaitGroup` + error channels for cancel-on-
error fan-out. Always use `errgroup.WithContext` so the derived context cancels siblings on
first failure.

```go
import "golang.org/x/sync/errgroup"

func fetchAll(ctx context.Context, urls []string) ([]string, error) {
    g, ctx := errgroup.WithContext(ctx)
    results := make([]string, len(urls))

    for i, url := range urls {
        g.Go(func() error {
            req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
            if err != nil {
                return err
            }
            resp, err := http.DefaultClient.Do(req)
            if err != nil {
                return err // first error cancels ctx; siblings observe via ctx.Done()
            }
            defer resp.Body.Close()
            body, err := io.ReadAll(resp.Body)
            if err != nil {
                return err
            }
            results[i] = string(body) // safe: each goroutine writes to a unique index
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return results, nil
}
```

Bound concurrency with `g.SetLimit(n)`:

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(8) // at most 8 goroutines at once
for _, item := range items {
    g.Go(func() error { return process(ctx, item) })
}
return g.Wait()
```

**When NOT to use errgroup:** If you want supervisor-like behavior (siblings keep running on
failure), use `sync.WaitGroup` with `wg.Go` instead (Pattern 15).

---

## 15. `sync.WaitGroup.Go` (Go 1.25+)

**Use when:** You want supervisor semantics \u2014 launch N goroutines, collect results/errors
without cancelling siblings on failure. Replaces the `wg.Add(1)` / `defer wg.Done()` boilerplate.

**Rule:** `wg.Go(fn)` handles `Add(1)` and `Done()` internally. Use it instead of manual
`Add`/`Done` for any new code targeting Go 1.25+.

```go
func processAll(items []string) []error {
    var (
        wg   sync.WaitGroup
        mu   sync.Mutex
        errs []error
    )

    for _, item := range items {
        wg.Go(func() { // Go 1.25+: no Add(1) / defer Done()
            if err := process(item); err != nil {
                mu.Lock()
                errs = append(errs, err)
                mu.Unlock()
            }
        })
    }

    wg.Wait()
    return errs
}
```

Common mistake `wg.Go` prevents: calling `wg.Add` inside the goroutine instead of before it,
which races with `wg.Wait`.

---

## 16. Request Coalescing with `singleflight`

**Use when:** Multiple concurrent goroutines request the same expensive resource (cache miss
stampede, duplicate DB queries, dogpile on a slow API). `singleflight` ensures only one call
executes; all callers receive the same result.

**Rule:** Use for read-through caches and any idempotent expensive lookup. Do NOT use for
mutations \u2014 coalescing two writes into one silently drops the second.

```go
import "golang.org/x/sync/singleflight"

type UserCache struct {
    sf    singleflight.Group
    cache sync.Map
}

func (c *UserCache) Get(ctx context.Context, id string) (*User, error) {
    if v, ok := c.cache.Load(id); ok {
        return v.(*User), nil
    }
    v, err, _ := c.sf.Do(id, func() (any, error) {
        u, err := fetchUserFromDB(ctx, id)
        if err != nil {
            return nil, err
        }
        c.cache.Store(id, u)
        return u, nil
    })
    if err != nil {
        return nil, err
    }
    return v.(*User), nil
}
```

`shared` (third return value) tells you whether your call's result was deduplicated with
others \u2014 useful for metrics.

---

## 17. Or-Channel: Merging Cancellation Signals

**Use when:** You need a single channel that closes as soon as ANY of N input channels close.
Useful for combining multiple cancellation sources (context, timeout, user signal, parent done).

**Rule:** Prefer `context.WithCancel` chains over hand-rolled or-channels when contexts are
available. Use this pattern only when you genuinely have raw `<-chan struct{}` signals.

```go
func or(channels ...<-chan struct{}) <-chan struct{} {
    switch len(channels) {
    case 0:
        return nil
    case 1:
        return channels[0]
    }
    out := make(chan struct{})
    go func() {
        defer close(out)
        switch len(channels) {
        case 2:
            select {
            case <-channels[0]:
            case <-channels[1]:
            }
        default:
            select {
            case <-channels[0]:
            case <-channels[1]:
            case <-channels[2]:
            case <-or(append(channels[3:], out)...): // recursive divide-and-conquer
            }
        }
    }()
    return out
}

// Usage:
done := or(ctx.Done(), timer.C, userCancel)
<-done // returns when ANY input closes
```

---

## Go 1.26+ Quick Reference

| Feature | Use |
|---------|-----|
| `wg.Go(fn)` (1.25+) | Replaces `wg.Add(1)` + `defer wg.Done()` boilerplate |
| `errgroup.Group.SetLimit` | Bound concurrency without a separate semaphore channel |
| Per-iteration loop vars (1.22+) | No more `v := v` shadowing inside `for _, v := range` |
| `runtime/pprof` `goroutineleak` profile (1.26 experimental) | Detect goroutines blocked on unreachable primitives in production via `/debug/pprof/goroutineleak` |
| `testing/synctest` (1.24+) | Test concurrent code with fake time \u2014 deterministic tests of timeouts and cancellation |
| `slog` + context | Use `slog.InfoContext(ctx, ...)` to propagate cancellation context into logs |

Detection commands:

```bash
go test -race ./...                                # data race detector
go test ./... -run . -tags goleak                  # uber-go/goleak in tests
curl http://localhost:6060/debug/pprof/goroutineleak  # Go 1.26 production leak profile
```
