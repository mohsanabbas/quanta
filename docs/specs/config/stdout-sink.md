# Stdout Sink Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `delay_ms` | `int` | No | Artificial delay before printing. |
| `print_counter` | `bool` | No | Log frame count/offset info. |
| `ack_batch_size` | `int` | No | Size before acknowledging batch. |
| `ack_flush_ms` | `int` | No | Timeout for ack flush. |
| `print_value` | `bool` | No | Log payload contents. |
| `value_max_bytes` | `int` | No | Truncate payload logs. |

## Example

```yaml
sink_configs:
  stdout:
    print_counter: true
    print_value: true
    value_max_bytes: 256
```

