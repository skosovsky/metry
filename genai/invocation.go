package genai

import (
	"context"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// AgentInvocation observes a caller-owned agent interval. ID is a stable agent
// identity (a hosted resource ID for remote execution), never a generated run ID.
// Provider is an optional caller fact; locality is always explicit.
type AgentInvocation struct {
	Name           string
	Provider       string
	ID             string
	Version        string
	ConversationID string
	Locality       ExecutionLocality
}

// WorkflowInvocation observes an explicit application workflow interval. It
// neither creates nor executes a workflow and does not aggregate child resources.
type WorkflowInvocation struct {
	Name           string
	ID             string
	Version        string
	ConversationID string
}

// InvocationResult contains the caller's outcome, without resource summaries.
type InvocationResult struct {
	Status     Outcome
	Err        error
	ErrorClass ErrorClass
}

// InvocationHandle records one interval and owns no business execution state.
type InvocationHandle struct {
	mu       sync.Mutex
	finished bool
	tracker  *Tracker
	ctx      context.Context
	span     trace.Span
	started  time.Time
	kind     string
	locality ExecutionLocality
}

func (t *Tracker) StartAgent(
	ctx context.Context,
	invocation AgentInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	return startAgent(ctx, t, invocation, options)
}

func (t *Tracker) StartWorkflow(
	ctx context.Context,
	invocation WorkflowInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	return startWorkflow(ctx, t, invocation, options)
}

func (r trackerRecorder) StartAgent(
	ctx context.Context,
	invocation AgentInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	if invocation.Provider == "" {
		invocation.Provider = r.provider
	}
	return startAgent(ctx, r.tracker, invocation, options)
}

func (r trackerRecorder) StartWorkflow(
	ctx context.Context,
	invocation WorkflowInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	return startWorkflow(ctx, r.tracker, invocation, options)
}

func (noopRecorder) StartAgent(
	ctx context.Context,
	invocation AgentInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	return startAgent(ctx, nil, invocation, options)
}

func (noopRecorder) StartWorkflow(
	ctx context.Context,
	invocation WorkflowInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	return startWorkflow(ctx, nil, invocation, options)
}

func startAgent(
	ctx context.Context,
	t *Tracker,
	invocation AgentInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	if err := invocation.Locality.Validate(); err != nil {
		return ctx, nil, err
	}
	if invocation.Provider == "" {
		if scope, ok := ScopeFromContext(ctx); ok {
			invocation.Provider = scope.Provider
		}
	}
	if invocation.Locality == ExecutionRemote && invocation.Provider == "" {
		invocation.Provider = unknownProviderName
	}
	err := validateInvocation(
		invocation.Name,
		invocation.ID,
		invocation.Version,
		invocation.ConversationID,
		invocation.Provider,
	)
	if err != nil {
		return ctx, nil, err
	}
	var attrs []attribute.KeyValue
	name := invocation.Name
	if t != nil {
		name = metadataWithConfig(AgentName, name, t.cfg)
		idKey := LocalAgentID
		if invocation.Locality == ExecutionRemote {
			idKey = AgentID
		}
		attrs = invocationAttributes(
			t,
			AgentName,
			name,
			idKey,
			invocation.ID,
			AgentVersion,
			invocation.Version,
			invocation.ConversationID,
		)
		if invocation.Provider != "" {
			attrs = append(
				attrs,
				attribute.String(ProviderName, metadataWithConfig(ProviderName, invocation.Provider, t.cfg)),
			)
		}
	}
	return startInvocation(ctx, t, "agent", "invoke_agent", name, invocation.Locality, attrs, options)
}

func startWorkflow(
	ctx context.Context,
	t *Tracker,
	invocation WorkflowInvocation,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	if err := validateInvocation(
		invocation.Name,
		invocation.ID,
		invocation.Version,
		invocation.ConversationID,
		"",
	); err != nil {
		return ctx, nil, err
	}
	var attrs []attribute.KeyValue
	name := invocation.Name
	if t != nil {
		name = metadataWithConfig(WorkflowName, name, t.cfg)
		attrs = invocationAttributes(
			t,
			WorkflowName,
			name,
			WorkflowID,
			invocation.ID,
			WorkflowVersion,
			invocation.Version,
			invocation.ConversationID,
		)
	}
	return startInvocation(ctx, t, "workflow", "invoke_workflow", name, ExecutionLocal, attrs, options)
}

func validateInvocation(name string, refs ...string) error {
	if name == "" || len(name) > maxMetricValueLength || !utf8.ValidString(name) {
		return invalidObservation("invocation", "malformed")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalidObservation("invocation", "malformed")
		}
	}
	for _, value := range refs {
		if !boundedIdentity(value) {
			return invalidObservation("invocation", "malformed")
		}
	}
	return nil
}

func invocationAttributes(
	t *Tracker,
	nameKey, name, idKey, id, versionKey, version, conversation string,
) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String(nameKey, name)}
	for _, item := range []struct{ key, value string }{{idKey, id}, {versionKey, version}, {ConversationID, conversation}} {
		if item.value != "" {
			attrs = append(attrs, attribute.String(item.key, referenceWithConfig(item.value, t.cfg)))
		}
	}
	return attrs
}

func startInvocation(
	ctx context.Context,
	t *Tracker,
	kind, operation, name string,
	locality ExecutionLocality,
	attrs []attribute.KeyValue,
	options RecordingOptions,
) (context.Context, *InvocationHandle, error) {
	if err := validateCausalRecordingOptions(t, options); err != nil {
		return ctx, nil, err
	}
	links, err := recordingLinks(ctx, options)
	if err != nil {
		return ctx, nil, err
	}
	started := time.Now()
	var span trace.Span
	if t != nil {
		attrs = append(
			attrs,
			attribute.String(OperationName, operation),
			attribute.String(InvocationKindKey, kind),
			attribute.String(ExecutionLocalityKey, string(locality)),
		)
		attrs = append(attrs, options.Identity.attributes(t.cfg)...)
		predecessor := options.Predecessor
		if predecessor == "" {
			predecessor = PredecessorUnknown
		}
		attrs = append(attrs, attribute.String("metry.causal.predecessor", string(predecessor)))
		spanOptions := []trace.SpanStartOption{
			trace.WithTimestamp(started),
			trace.WithSpanKind(locality.spanKind()),
			trace.WithAttributes(attrs...),
			trace.WithLinks(links...),
		}
		if options.Detached {
			spanOptions = append(spanOptions, trace.WithNewRoot())
		}
		ctx, span = t.tracer.Start(ctx, operation+" "+name, spanOptions...)
	}
	return ctx, &InvocationHandle{
		mu:       sync.Mutex{},
		finished: false,
		tracker:  t,
		ctx:      ctx,
		span:     span,
		started:  started,
		kind:     kind,
		locality: locality,
	}, nil
}

// Finish validates before writes. Invalid results remain correctable; successful
// finalization is once-only and takes precedence over later input validation.
func (h *InvocationHandle) Finish(result InvocationResult) error {
	if h == nil || h.started.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	status, class, err := resolveToolOutcome(h.tracker, result.Status, result.Err, result.ErrorClass)
	if err != nil {
		return err
	}
	h.finished = true
	if h.tracker != nil {
		finished := time.Now()
		applyOutcome(h.span, status, class)
		h.recordDuration(finished.Sub(h.started), status)
		h.span.End(trace.WithTimestamp(finished))
	}
	return nil
}

// Close records an unknown outcome without inferring execution success.
func (h *InvocationHandle) Close() error {
	return h.Finish(InvocationResult{Status: OperationStatusUnset, Err: nil, ErrorClass: ""})
}

func (h *InvocationHandle) recordDuration(elapsed time.Duration, status string) {
	if h.tracker.metrics == nil || h.tracker.metrics.InvocationDuration == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(InvocationKindKey, h.kind),
		attribute.String(ExecutionLocalityKey, string(h.locality)),
		attribute.String(OperationStatus, status),
	}
	h.tracker.metrics.InvocationDuration.Record(
		h.ctx,
		elapsed.Seconds(),
		metric.WithAttributes(h.tracker.boundedMetricAttributes(InvocationDurationMetricName, attrs)...),
	)
}
