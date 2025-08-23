// Package transport hosts the engine's gRPC surface (control/health/etc.).
// Engine.Bootstrap starts a Server and Engine.Run serves it until shutdown.
// Clients can be added later for admin/CLI; for now only the server is used.
package transport
