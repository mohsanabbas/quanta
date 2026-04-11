# Kafka Source Tuning Guide

##  How Tuning Works in the Pipeline

### Configuration Architecture

```
pipeline.docker.yml
  └─> source.config: "kafka_source.docker.yml"  ← Main config
         └─> Auto-loads: "kafka_source.docker.tuning.yml"  ← Tuning file
```

The tuning file is **automatically loaded** by inserting `.tuning` before the extension:
- `kafka_source.docker.yml` → `kafka_source.docker.tuning.yml`
- `kafka_source.yml` → `kafka_source.tuning.yml`
- `topology/prod.yaml` → `topology/prod.tuning.yaml`

### File Structure

**Main Config (`kafka_source.docker.yml`):**
- Broker addresses, topics, group ID
- Commit mode (auto vs e2e)
- TLS/SASL settings
- Strategy selections

**Tuning Config (`kafka_source.docker.tuning.yml`):**
- `inflight_bytes` - Memory limits
- `inflight_msgs` - Concurrency limits
- `window_bits` - Checkpoint window size
- `commit_interval` - Time-based commit frequency
- `commit_step` - Offset-based commit frequency

---

##  Quick Start: Using Tuning in Pipeline

### Option 1: Use Existing Configs (Recommended)

```yaml
# pipeline.docker.yml
source:
  kind: kafka
  driver: sarama
  config: kafka_source.docker.yml  # ← Main config
  # Automatically loads: kafka_source.docker.tuning.yml
```

 **That's it!** The tuning file is loaded automatically.

### Option 2: Override with Environment Variables

```bash
# Override tuning without editing files
export QUANTA_TUNING__INFLIGHT_MSGS=8000
export QUANTA_TUNING__COMMIT_INTERVAL=10s

docker-compose up -d
```

### Option 3: Create Custom Configs

```yaml
# pipeline.production.yml
source:
  kind: kafka
  driver: sarama
  config: kafka_source.production.yml
```

Then create both files:
- `kafka_source.production.yml` (main config)
- `kafka_source.production.tuning.yml` (tuning)

---

##  Tuning Parameters Explained

### 1. `inflight_bytes` (Memory Limit)

**What it does:** Maximum bytes held in memory before blocking

**E2E Mode:**
```
Memory = avg_msg_size × inflight_msgs × num_partitions
```

**Auto Mode:**
```
Memory = avg_msg_size × pipeline_depth
```
(Much lower since tokens released immediately)

**Tuning Guidelines:**
| Message Size | E2E `inflight_bytes` | Auto `inflight_bytes` |
|--------------|----------------------|----------------------|
| < 1KB        | 128 MiB             | 512 MiB             |
| 1-10KB       | 256 MiB             | 512 MiB             |
| 10-100KB     | 512 MiB             | 1 GiB               |
| > 100KB      | 1 GiB               | 2 GiB               |

### 2. `inflight_msgs` (Concurrency Limit)

**What it does:** Maximum unacknowledged messages

**E2E Mode:** Held until sink acks
**Auto Mode:** Held only during emit

**Tuning Guidelines:**
| Throughput Need | E2E Value | Auto Value |
|-----------------|-----------|-----------|
| Low (< 100/s)   | 1000      | 5000      |
| Medium (100-1K/s)| 4096     | 10000     |
| High (> 1K/s)   | 8192      | 20000     |

**Formula:**
```
inflight_msgs = target_throughput × processing_latency_seconds
```

Example: 500 msg/s × 3s latency = 1500 messages

### 3. `window_bits` (Checkpoint Window)

**E2E Mode Only** (ignored in auto mode)

**What it does:** Sliding window size for tracking out-of-order acks

**Rule:** `window_bits >= inflight_msgs`

**Recommended:** `window_bits = 2 × inflight_msgs`

**Why?**
- Handles async processing where acks arrive out of order
- Prevents window full errors during bursts

### 4. `commit_interval` (Time-Based Commits)

**What it does:** Maximum time between commits

**E2E Mode:**
- Commits happen even if base doesn't advance
- Prevents stuck partitions during out-of-order acks
- Lower = more frequent commits, safer but slower

**Auto Mode:**
- Sarama's built-in auto-commit interval
- Higher values OK since offsets marked immediately

**Tuning Guidelines:**
| Safety Need | E2E Value | Auto Value |
|-------------|-----------|-----------|
| Maximum     | 2s        | 30s       |
| Balanced    | 5s        | 10s       |
| Performance | 15s       | 60s       |

**Trade-off:**
- Lower: More commits, less replay on crash, higher overhead
- Higher: Fewer commits, more replay on crash, better throughput

### 5. `commit_step` (Offset-Based Commits)

**What it does:** Commit when base advances by N offsets

**E2E Mode:** Used with hybrid commit strategy
**Auto Mode:** Not used (offsets marked immediately)

**Tuning Guidelines:**
```
commit_step = inflight_msgs / 4  (for frequent commits)
commit_step = inflight_msgs / 2  (balanced)
commit_step = inflight_msgs      (infrequent commits)
```

---

##  Pre-Configured Scenarios

### Scenario 1: Maximum Safety (E2E)
```yaml
# kafka_source.docker.safe.yml
commit_mode: "e2e"
backpressure_strategy: "combined"
checkpoint_strategy: "sliding_window"
commit_strategy_type: "hybrid"
```

```yaml
# kafka_source.docker.safe.tuning.yml
inflight_bytes: 67108864   # 64 MiB
inflight_msgs: 1000         # Limited in-flight
window_bits: 2048           # 2× inflight_msgs
commit_interval: 2s         # Very frequent commits
commit_step: 50             # Small batches
```

**Use When:**
- Cannot tolerate message loss
- Can handle duplicates on restart
- Lower throughput acceptable

### Scenario 2: Balanced Performance (E2E)
```yaml
# kafka_source.docker.yml
commit_mode: "e2e"
```

```yaml
# kafka_source.docker.tuning.yml
inflight_bytes: 268435456   # 256 MiB
inflight_msgs: 4096          # Good parallelism
window_bits: 8192            # 2× inflight_msgs
commit_interval: 5s          # Balanced frequency
commit_step: 500             # Moderate batching
```

**Use When:**
- Need reliability with good performance
- **Recommended starting point**

### Scenario 3: Maximum Throughput (Auto)
```yaml
# kafka_source.docker.auto.yml
commit_mode: "auto"
backpressure_strategy: "count"  
```

```yaml
# kafka_source.docker.auto.tuning.yml
inflight_bytes: 536870912   # 512 MiB
inflight_msgs: 10000         # High concurrency
window_bits: 16384           # Not used but required
commit_interval: 10s         # Infrequent commits
commit_step: 500             # Not used
```

**Use When:**
- Speed is priority
- Some message loss acceptable
- Fire-and-forget workload

---

##  Troubleshooting

### Problem: Partition Stuck with Lag

**Symptoms:**
- One partition shows lag but others are fine
- No errors in logs
- Acks arriving but lag not clearing

**Cause:** Out-of-order acks preventing base from advancing

**Solution:**
```yaml
# Increase commit_interval for more frequent time-based commits
commit_interval: 2s  # Down from 5s

# Or decrease commit_step for more frequent offset-based commits
commit_step: 100  # Down from 500
```

### Problem: High Memory Usage

**Symptoms:**
- Engine consuming too much memory
- OOM errors

**Solution:**
```yaml
# Reduce in-flight limits
inflight_bytes: 134217728  # 128 MiB (down from 256 MiB)
inflight_msgs: 2000         # Down from 4096
window_bits: 4096           # Down from 8192
```

### Problem: Low Throughput

**Symptoms:**
- Processing slower than expected
- Pipeline not saturated

**Solution:**
```yaml
# Increase concurrency
inflight_msgs: 8192         # Up from 4096
window_bits: 16384          # Up from 8192

# Less frequent commits
commit_interval: 10s        # Up from 5s
commit_step: 1000           # Up from 500
```

### Problem: "Window Full" Errors

**Symptoms:**
```
ERROR: checkpoint window full
```

**Cause:** `window_bits < inflight_msgs` or burst processing

**Solution:**
```yaml
# Ensure window is larger than in-flight
window_bits: 8192           # Must be >= inflight_msgs
inflight_msgs: 4096
# Or reduce in-flight messages
```

### Problem: Duplicate Messages on Restart

**Symptoms:**
- Same messages are processed multiple times after crash/restart

**Cause:** Normal in E2E mode (at-least-once semantics)

**Solutions:**

1. **Make downstream idempotent** (best practice)
2. **Increase commit frequency:**
   ```yaml
   commit_interval: 2s      # More frequent commits
   commit_step: 100         # Smaller batches
   ```
3. **Switch to auto mode** (if loss acceptable):
   ```yaml
   commit_mode: "auto"
   ```

---



### Test 2: Auto Mode (High Throughput)
```bash
# Edit topology/pipeline.docker.yml
#   config: kafka_source.docker.auto.yml
docker-compose down
docker-compose up -d
```

**Expected Behavior:**
- Much faster processing
- No "base advance" logs
- Offsets marked immediately

### Test 3: Safe Mode (Maximum Safety)
```bash
# Edit topology/pipeline.docker.yml
#   config: kafka_source.docker.safe.yml
docker-compose down
docker-compose up -d
```

**Expected Behavior:**
- Slower but very frequent commits
- Small lag even with processing
- Minimal replay on crash

---

##  Monitoring

### Key Metrics to Watch

1. **Consumer Lag** (Kafka UI)
   - Should decrease steadily
   - Spikes normal during rebalance

2. **Commit Frequency** (Logs)
   ```
   docker-compose logs engine | grep "committing" | wc -l
   ```

3. **Memory Usage**
   ```
   docker stats engine
   ```

4. **Throughput**
   ```
   # Messages per second
   docker-compose logs engine | grep "print_counter"
   ```

### Healthy Indicators

✅ Lag decreasing at steady rate  
✅ Commits every 5-10 seconds  
✅ Memory stable (not growing)  
✅ No "window full" errors  
✅ Throughput matches expectations  

### Warning Signs

⚠️ Lag stuck on one partition  
⚠️ No commits for > 30 seconds  
⚠️ Memory constantly growing  
⚠️ Frequent "window full" errors  
⚠️ Throughput << broker capacity  

---

##  Recommended Starting Configuration

```yaml
# kafka_source.docker.yml
schema_version: v1
brokers: ["kafka:29092"]
topics: ["your-topic"]
group_id: "quanta-consumer"
start_from: "newest"
version: "3.6.0"
commit_mode: "e2e"
backpressure_strategy: "combined"
checkpoint_strategy: "sliding_window"
commit_strategy_type: "hybrid"
```

```yaml
# kafka_source.docker.tuning.yml
inflight_bytes: 268435456   # 256 MiB
inflight_msgs: 4096
window_bits: 8192
commit_interval: 5s
commit_step: 500
```

**Then tune based on monitoring!**

---

##  Related Files

- `topology/kafka_source.recipes.yml` - Complete configuration recipes
- `topology/kafka_source.tuning.yml` - Tuning reference with calculations
- `CONFIGS.md` - General configuration guide
- `docs/specs/source.md` - Source specification

