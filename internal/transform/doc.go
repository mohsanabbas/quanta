// Package transform defines the engine-side client interface for external
// transformers (e.g., gRPC plugins). Runner stages use a transform.Client
// to invoke plugins with timeouts, retries, and close lifecycle.
package transform
