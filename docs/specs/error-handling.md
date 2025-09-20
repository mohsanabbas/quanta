# Error Handling

| Stage Outcome | Commit Offset? | Retry? | Notes |
|---------------|----------------|--------|-------|
| Transform success → Sink success | Yes | No | Standard flow. |
| Transform error | No (E2E) | Yes, bounded by stage config | Offset is withheld until retry succeeds. After exhaustion, frame is acknowledged (future DLQ support). |
| Sink error | No (E2E) | Planned retry/backoff | Currently surfaces error and halts pipeline; upcoming milestones add retry + DLQ. |
| Transformer `DROP` | Yes | No | Treated as successful filter. |
| Context cancelled | No | No | Shutdown aborts in-flight work without committing. |

## Hooks

- Stage configuration controls retry count and backoff duration.
- Sink adapters will expose retry/backoff knobs (`retry.*`) and DLQ settings in later milestones.
- Metrics: upcoming work adds counters for publish attempts/errors, retry totals, and DLQ writes.

