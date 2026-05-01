// Package batch provides reusable batching for streaming sinks.
//
// Components: [Batch] accumulates records, [Flusher] manages flush lifecycle,
// [Pool] enables batch reuse. Guarantees at-least-once delivery via ack/nack.
package batch
