package traceutil

const (
	// MaxTokenBytes bounds encoded trace-only continuation tokens before decode.
	MaxTokenBytes = 2048
	// MaxTraceStateBytes is the W3C serialized tracestate budget.
	MaxTraceStateBytes = 512
)
