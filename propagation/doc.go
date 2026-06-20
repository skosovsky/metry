// Package propagation bridges OpenTelemetry trace context through protocol-level map carriers.
// For durable queues, jobs, and checkpoints, prefer metry.TraceSnapshot instead of storing carriers.
package propagation
