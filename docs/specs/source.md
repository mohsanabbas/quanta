# Source Specification

Sources implement the `Adapter` interface:

```go
type Adapter interface {
    Configure(ctx context.Context, cfg Config) error
    Run(ctx context.Context, emit func(context.Context, *pb.Frame) error) error
    Close(ctx context.Context) error
}
```

## Registration

Drivers register themselves via `source/kafka.Register()`. At build time the pipeline asks the registry for the named adapter and configures it.

## Emit Contract

- `emit` must be called synchronously for each inbound message.
- The provided `context.Context` propagates cancellation from the broker (e.g., Sarama session) into the pipeline.
- If `emit` returns an error, the source should stop consuming and propagate the error upstream.

## Blocking & Backpressure

Sources must acquire inflight capacity before calling `emit`. The Kafka driver supports three backpressure strategies:

### Backpressure Strategies

**Combined** (default):
- Enforces both byte and message count limits
- Acquires tokens from both semaphores
- Use when: Need to control both memory and concurrency

**Count**:
- Only enforces message count limits
- Ignores message sizes
- Use when: Messages are uniform size

**Size**:
- Only enforces byte size limits
- Use when: Message sizes vary widely

The strategy is configured via `backpressure_strategy` in the main config. Tuning parameters `inflight_bytes` and `inflight_msgs` control the limits.

During shutdown, sources must honour `ctx.Done()` and stop emitting promptly. Backpressure tokens are released when:
1. Acks arrive (E2E mode)
2. Emit completes (Auto mode)
3. Emit fails (error cleanup)

## Checkpoint Management

In E2E mode, each partition maintains a **sliding window checkpoint manager** to track in-flight messages:

- **Window Size**: Configured via `window_bits` (must be ≥ `inflight_msgs`)
- **Out-of-Order Acks**: The window handles acks arriving out of order
- **Base Advancement**: Only advances when all earlier offsets are acked
- **Window Full**: If the window fills, the reader pauses until downstream frees space

The checkpoint strategy is configured via `checkpoint_strategy` (currently only `sliding_window` is supported).

## Offset Handling & Commit Strategies

### Auto Mode
- Offsets marked immediately after emit to pipeline
- No checkpoint tracking
- Sarama commits marked offsets periodically
- ⚠️ Message loss possible in a crash
- ✅ Maximum throughput

### E2E Mode
- Waits for sink acknowledgments before committing
- Checkpoint manager tracks all in-flight offsets
- Three commit strategies are available:

**Hybrid** (default):
- Commits on base advance (when the checkpoint window moves forward)
- Plus, periodic commits are based on a time interval
- Best balance of safety and performance

**Periodic**:
- Only commits based on a time interval
- Simpler but may commit less frequently

**Immediate**:
- Commits on every base advance
- Maximum safety but higher overhead

The commit strategy is configured via `commit_strategy_type` in the main config. Tuning parameters:
- `commit_interval`: Time between periodic commits (e.g., `5s`)
- `commit_step`: Minimum offset advance to trigger commit (e.g., `500`)

### Configuration Invariants

- `window_bits ≥ 256`
- `inflight_msgs ≤ window_bits` (window must accommodate all in-flight messages)
- `commit_interval > 0`
- `commit_step > 0`

See [configuration.md](configuration.md) for full configuration details and [../guides/TUNING_GUIDE.md](../guides/TUNING_GUIDE.md) for performance tuning guidance.
