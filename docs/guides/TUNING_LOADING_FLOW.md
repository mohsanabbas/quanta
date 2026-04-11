# How Tuning Files Are Loaded - Complete Flow

##  Quick Answer

**WHO:** The `LoadConfig()` function in `source/kafka/config.go`  
**WHEN:** During pipeline compilation/bootstrap  
**WHERE:** Automatically alongside the main config file  
**HOW:** By inserting `.tuning` before the file extension  

---

##  Complete Loading Flow

### 1. Engine Starts
```
cmd/engine/main.go
  └─> Reads QUANTA_PIPELINE_YML environment variable
      └─> Default: /topology/pipeline.docker.yml
```

### 2. Pipeline Compilation
```go
// internal/pipeline/compiler.go:17
func Compile(ctx context.Context, path string) (*Runner, error) {
    r := NewRunner()
    if err := LoadYAML(ctx, path, r); err != nil {
        return nil, err
    }
    return r, nil
}
```

### 3. Load Pipeline Spec
```go
// internal/pipeline/compiler.go:23
func LoadYAML(ctx context.Context, path string, r *Runner) error {
    registerBuiltins()
    
    // Load pipeline.docker.yml
    cfg, err := config.LoadPipelineSpec(path)
    if err != nil {
        return err
    }
    
    // cfg.Source.Config = "kafka_source.docker.yml"
    // cfg.Source.ResolvedConfigPath() = "/topology/kafka_source.docker.yml"
    
    // ...
}
```

### 4. Load Kafka Config (Main + Tuning)
```go
// internal/pipeline/compiler.go:37
kc, err := config.LoadKafkaConfig(cfg.Source.ResolvedConfigPath())
if err != nil {
    return err
}
```

This calls:
```go
// internal/config/kafka.go:7
func LoadKafkaConfig(path string) (kcfg.Config, error) {
    return kcfg.LoadConfig(path)
}
```

### 5. The Magic Happens Here! 
```go
// source/kafka/config.go:89
func LoadConfig(path string) (Config, error) {
    var cfg Config
    
    // Load main config (kafka_source.docker.yml)
    public, err := loadPublicConfig(path)
    if err != nil {
        return cfg, err
    }
    
    // Load tuning config (kafka_source.docker.tuning.yml) ← AUTOMATIC!
    tuning, err := loadTuningConfig(path)
    if err != nil {
        return cfg, err
    }
    
    cfg.public = public
    cfg.tuning = tuning
    return cfg, nil
}
```

### 6. Loading Tuning Config
```go
// source/kafka/config.go:124
func loadTuningConfig(publicPath string) (Tuning, error) {
    k := koanf.New(".")
    
    // Derive tuning path from main config path
    if publicPath != "" {
        tuningPath := deriveTuningPath(publicPath)
        // publicPath: "/topology/kafka_source.docker.yml"
        // tuningPath: "/topology/kafka_source.docker.tuning.yml"
        
        // Check if tuning file exists
        if _, err := os.Stat(tuningPath); err == nil {
            // Load the tuning file
            if err := k.Load(file.Provider(tuningPath), yaml.Parser()); err != nil {
                return Tuning{}, err
            }
        } else if !errors.Is(err, os.ErrNotExist) {
            return Tuning{}, err
        }
        // If file doesn't exist, that's OK - defaults will be used
    }
    
    // Allow environment variables to override
    // QUANTA_TUNING__INFLIGHT_MSGS=8000 → inflight_msgs: 8000
    if err := k.Load(env.Provider("QUANTA_TUNING__", "__", tuningEnvKey), nil); err != nil {
        return Tuning{}, err
    }
    
    var tuning Tuning
    if err := k.Unmarshal("", &tuning); err != nil {
        return tuning, err
    }
    
    // Apply defaults for any missing values
    applyTuningDefaults(&tuning)
    
    // Validate the tuning parameters
    if err := validateTuning(tuning); err != nil {
        return tuning, err
    }
    
    return tuning, nil
}
```

### 7. Path Derivation Logic
```go
// source/kafka/config.go:163
func deriveTuningPath(publicPath string) string {
    if publicPath == "" {
        return ""
    }
    ext := filepath.Ext(publicPath)
    base := strings.TrimSuffix(publicPath, ext)
    
    if ext == "" {
        return base + ".tuning"
    }
    return base + ".tuning" + ext
}
```

**Examples:**
| Input Path | Tuning Path |
|------------|-------------|
| `/topology/kafka_source.docker.yml` | `/topology/kafka_source.docker.tuning.yml` |
| `/topology/kafka_source.docker.auto.yml` | `/topology/kafka_source.docker.auto.tuning.yml` |
| `kafka_source.yml` | `kafka_source.tuning.yml` |
| `topology/prod.yaml` | `topology/prod.tuning.yaml` |
| `myconfig` | `myconfig.tuning` |

---

##  Visual Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. ENGINE STARTS                                                │
│    cmd/engine/main.go                                           │
│    Reads: QUANTA_PIPELINE_YML=/topology/pipeline.docker.yml      │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. COMPILE PIPELINE                                             │
│    internal/pipeline/compiler.go:Compile()                      │
│    └─> LoadYAML()                                               │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. LOAD PIPELINE SPEC                                           │
│    config.LoadPipelineSpec("/topology/pipeline.docker.yml")      │
│    Returns:                                                     │
│      - cfg.Source.Config = "kafka_source.docker.yml"           │
│      - cfg.Source.ResolvedConfigPath() = "/config/kafka_..."   │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. LOAD KAFKA CONFIG                                            │
│    config.LoadKafkaConfig(cfg.Source.ResolvedConfigPath())      │
│    └─> kafka.LoadConfig()                                       │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ├──────────────────────────┬──────────────┐
                         ▼                          ▼              ▼
              ┌──────────────────────┐   ┌─────────────────────────────────┐
              │ 5A. LOAD PUBLIC      │   │ 5B. LOAD TUNING (AUTOMATIC!)    │
              │ loadPublicConfig()   │   │ loadTuningConfig()              │
              │                      │   │                                 │
              │ Reads:               │   │ Derives path:                   │
              │ kafka_source.        │   │ kafka_source.docker.yml         │
              │   docker.yml         │   │   ↓ deriveTuningPath()          │
              │                      │   │ kafka_source.docker.tuning.yml  │
              │ Fields:              │   │                                 │
              │ - brokers            │   │ Checks if file exists:          │
              │ - topics             │   │ os.Stat(tuningPath)             │
              │ - group_id           │   │   ↓ if exists                   │
              │ - commit_mode        │   │ Loads YAML:                     │
              │ - backpressure_      │   │ k.Load(file.Provider())         │
              │   strategy           │   │                                 │
              │ - etc.               │   │ Fields:                         │
              │                      │   │ - inflight_bytes                │
              │ + ENV overrides:     │   │ - inflight_msgs                 │
              │ QUANTA_SOURCE__*     │   │ - window_bits                   │
              │                      │   │ - commit_interval               │
              │                      │   │ - commit_step                   │
              │                      │   │                                 │
              │                      │   │ + ENV overrides:                │
              │                      │   │ QUANTA_TUNING__*                │
              │                      │   │                                 │
              │                      │   │ If file missing:                │
              │                      │   │ → Use defaults                  │
              └──────────┬───────────┘   └──────────┬──────────────────────┘
                         │                          │
                         └──────────┬───────────────┘
                                    ▼
              ┌─────────────────────────────────────────────┐
              │ 6. COMBINE INTO Config STRUCT               │
              │    Config {                                 │
              │      public: PublicConfig { ... }           │
              │      tuning: Tuning { ... }                 │
              │    }                                        │
              └─────────────────┬───────────────────────────┘
                                ▼
              ┌─────────────────────────────────────────────┐
              │ 7. CONFIGURE DRIVER                         │
              │    driver.Configure(ctx, config)            │
              │    source/kafka/driver_sarama.go:41         │
              │                                             │
              │    Uses both public and tuning:             │
              │    - pub := cfg.Public()                    │
              │    - tun := cfg.Tuning()                    │
              │                                             │
              │    Creates backpressure manager with:       │
              │    - tun.InFlightBytes                      │
              │    - tun.InFlightMsgs                       │
              │                                             │
              │    Creates checkpoint manager with:         │
              │    - tun.WindowBits                         │
              │                                             │
              │    Creates commit strategy with:            │
              │    - tun.CommitInterval                     │
              │    - tun.CommitStep                         │
              └─────────────────────────────────────────────┘
```

---

##  File Locations in Docker Container

When engine runs in Docker, the volumes are mounted:

```yaml
# docker-compose.yml
volumes:
  - ./topology:/config:ro
```

**Inside Container:**
```
/config/
  ├── pipeline.docker.yml              ← Entry point
  ├── kafka_source.docker.yml          ← Main config (loaded explicitly)
  └── kafka_source.docker.tuning.yml  ← Tuning (loaded automatically)
```

---

##  Key Points

### 1. **Automatic Discovery**
You never specify the tuning file path. The system automatically derives it:
```go
"/topology/kafka_source.docker.yml" → "/topology/kafka_source.docker.tuning.yml"
```

### 2. **Optional**
If the tuning file doesn't exist, it's not an error. Defaults are applied:
```go
if _, err := os.Stat(tuningPath); err == nil {
    // Load it
} else if !errors.Is(err, os.ErrNotExist) {
    return Tuning{}, err
}
// else: file doesn't exist, use defaults
```

### 3. **Environment Override Priority**
```
1. Defaults (lowest priority)
   ↓
2. Tuning YAML file
   ↓
3. Environment variables (highest priority)
   QUANTA_TUNING__INFLIGHT_MSGS=8000
```

### 4. **Must Be Mounted in Docker**
For Docker deployments, mount the `topology/` directory:
```yaml
volumes:
  - ./topology:/config:ro
```

---

##  Verification

### Check What Gets Loaded

Add this to `source/kafka/config.go` after line 99:
```go
func LoadConfig(path string) (Config, error) {
    var cfg Config
    public, err := loadPublicConfig(path)
    if err != nil {
        return cfg, err
    }
    tuning, err := loadTuningConfig(path)
    if err != nil {
        return cfg, err
    }
    
    // DEBUG: Print what was loaded
    fmt.Printf("DEBUG: Loaded config from: %s\n", path)
    fmt.Printf("DEBUG: Tuning path derived: %s\n", deriveTuningPath(path))
    fmt.Printf("DEBUG: Tuning values:\n")
    fmt.Printf("  - inflight_bytes: %d\n", tuning.InFlightBytes)
    fmt.Printf("  - inflight_msgs: %d\n", tuning.InFlightMsgs)
    fmt.Printf("  - window_bits: %d\n", tuning.WindowBits)
    fmt.Printf("  - commit_interval: %s\n", tuning.CommitInterval)
    fmt.Printf("  - commit_step: %d\n", tuning.CommitStep)
    
    cfg.public = public
    cfg.tuning = tuning
    return cfg, nil
}
```

Then run:
```bash
docker-compose down
docker-compose build engine
docker-compose up engine
```

You'll see:
```
DEBUG: Loaded config from: /topology/kafka_source.docker.yml
DEBUG: Tuning path derived: /topology/kafka_source.docker.tuning.yml
DEBUG: Tuning values:
  - inflight_bytes: 268435456
  - inflight_msgs: 4096
  - window_bits: 8192
  - commit_interval: 5s
  - commit_step: 500
```

---

##  Summary

**Loading happens in this order:**

1. **Engine starts** → Reads `QUANTA_PIPELINE_YML`
2. **Pipeline compiled** → Loads `pipeline.docker.yml`
3. **Source config extracted** → Gets `kafka_source.docker.yml` path
4. **LoadConfig called** → `source/kafka/config.go:89`
5. **Main config loaded** → Reads `kafka_source.docker.yml`
6. **Tuning path derived** → Automatically computes `kafka_source.docker.tuning.yml`
7. **Tuning loaded** → If file exists, reads it; otherwise uses defaults
8. **Env vars applied** → `QUANTA_TUNING__*` overrides
9. **Combined Config returned** → Contains both public and tuning
10. **Driver configured** → Uses the combined config

**The key function:** `loadTuningConfig()` in `source/kafka/config.go:124`  
**The derivation logic:** `deriveTuningPath()` in `source/kafka/config.go:163`
