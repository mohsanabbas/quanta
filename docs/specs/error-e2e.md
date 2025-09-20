# Error Handling & E2E Semantics

The table below captures current behaviour in E2E mode:

| Stage Outcome | Commit Offset? | Retry? | DLQ? | Notes |
|---------------|----------------|--------|------|-------|
| Transform OK → Sink OK | Yes | No | No | Normal flow; sink acknowledgement triggers commit. |
| Transform ERROR | No | Yes (bounded by stage config) | Not yet (future DLQ) | Runner retries per transformer policy; on exhaustion the frame is acknowledged to unlock the source, matching current implementation. |
| Sink ERROR | No | Surface error and stop pipeline | Not yet | Current sink errors bubble up; the source stops and offsets remain uncommitted. |
| Transformer `DROP` | Yes | No | Optional (future) | Treated as successful filter. |
| Context cancelled mid-flight | No | No | No | Shutdown propagates cancellation; outstanding frames are not committed. |

## Unified Policy

- **Retry Budget** – configured per transformer stage via `retry_policy`. Sink retry orchestration is planned and will use the same context-aware interface.
- **Backoff** – linear backoff is controlled by `retry_policy.backoff_ms`.
- **Observability** – failures are logged via the logging package; metrics hooks will surface per-stage counters.
- **DLQ** – configuration placeholders exist in the spec reference; implementation is pending.

## Future Work

- Implement sink-level retry/DLQ as described in the error handling spec and update tests.
- Extend metrics to expose `publish_errors_total`, `transform_retries_total`, and DLQ statistics.

