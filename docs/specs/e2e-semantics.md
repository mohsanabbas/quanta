# E2E Offset Commit Semantics

| Scenario | Current Behaviour | Target Policy |
|----------|-------------------|---------------|
| Transform OK → Sink OK | Offsets committed after sink acknowledgement. | Keep. |
| Transform returns error | Runner retries per stage policy, then acknowledges after exhaustion (no commit). | Commit only after sink success; retry budget remains. |
| Sink returns error | Runner propagates error; source stops; offset not committed. | Retry sink publish, send to DLQ if configured, commit only after success. |
| Transformer DROP | Frame acknowledged immediately; offset committed. | Keep. |
| Context cancellation | No commit; outstanding frames dropped. | Keep. |

Tests under `internal/e2e` capture the current behaviour. Deviations from the planned policy are documented; implementing sink retry/DLQ is future work.

