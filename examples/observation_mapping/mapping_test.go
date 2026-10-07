package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/metrytest"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const sentinel = "private-mapping-sentinel"

type fixture struct {
	tracker  *genai.Tracker
	provider *metry.Provider
	sdk      *sdktrace.TracerProvider
	reader   *sdkmetric.ManualReader
	spans    *tracetest.InMemoryExporter
}

func newFixture(t *testing.T, opts ...genai.Option) fixture {
	t.Helper()
	spans := tracetest.NewInMemoryExporter()
	sdk := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, sdk.Shutdown(context.Background()))
		require.NoError(t, meter.Shutdown(context.Background()))
	})
	p, err := metry.FromSDK(
		metry.SDKDependencies{TracerProvider: sdk, MeterProvider: meter, Propagator: propagation.TraceContext{}},
	)
	require.NoError(t, err)
	tracker, err := genai.NewTrackerFromProvider(p, opts...)
	require.NoError(t, err)
	return fixture{tracker: tracker, provider: p, sdk: sdk, reader: reader, spans: spans}
}

func (f fixture) metrics(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, f.reader.Collect(t.Context(), &data))
	for _, scope := range data.ScopeMetrics {
		require.Equal(t, "metry/genai", scope.Scope.Name)
		require.Equal(t, "semconv-genai-e07f4eb+metry-v1", scope.Scope.Version)
		require.Equal(t, "https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev", scope.Scope.SchemaURL)
	}
	return data
}

func value(t *testing.T, attrs []attribute.KeyValue, key string) attribute.Value {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value
		}
	}
	t.Fatalf("attribute missing: %s", key)
	return attribute.Value{}
}

func absent(t *testing.T, attrs []attribute.KeyValue, key string) {
	t.Helper()
	for _, a := range attrs {
		require.NotEqual(t, key, string(a.Key))
	}
}

func fingerprint(s string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s))) }

func assertTokens(t *testing.T, data metricdata.ResourceMetrics, name string, sum int64, count uint64) {
	t.Helper()
	h := metrytest.FindInt64Histogram(t, data, metrytest.MetricSelector{Name: name})
	var total int64
	var n uint64
	for _, dp := range h.DataPoints {
		total += dp.Sum
		n += dp.Count
	}
	require.Equal(t, sum, total)
	require.Equal(t, count, n)
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				require.Equal(t, "{token}", m.Unit)
			}
		}
	}
}

func TestModelMappingExportTable(t *testing.T) {
	for _, tc := range []struct {
		state       string
		businessErr error
		status      codes.Code
		class       string
	}{
		{"completed", nil, codes.Ok, ""}, {"refusal", nil, codes.Ok, ""}, {"incomplete", nil, codes.Ok, ""},
		{"failed", nil, codes.Error, "unknown"}, {"cancelled", nil, codes.Error, "cancelled"},
		{"deadline", nil, codes.Error, "deadline_exceeded"}, {"unknown", nil, codes.Unset, ""},
		{"completed", errors.New(sentinel), codes.Error, "unknown"}, {"refusal", context.Canceled, codes.Error, "cancelled"},
		{"incomplete", context.DeadlineExceeded, codes.Error, "deadline_exceeded"}, {"unknown", errors.New(sentinel), codes.Error, "unknown"},
	} {
		t.Run(tc.state+"/"+tc.class, func(t *testing.T) {
			// Arrange: domain failure is independent of transport and result+error retains usage.
			f := newFixture(t, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
			domain := false
			original := modelResult{
				state:         tc.state,
				domainSuccess: &domain,
				err:           tc.businessErr,
				response:      sentinel,
				usage: hostUsage{
					input:  reported(0, "tokens", "observed"),
					output: reported(7, "tokens", "reported"),
				},
			}
			// Act.
			mapped, err := mapModel(original)
			require.NoError(t, err)
			require.NoError(
				t,
				f.tracker.Recorder().
					RecordOperation(t.Context(), genai.Operation{Name: "chat", Model: sentinel}, mapped.result, mapped.attributes...),
			)
			// Assert on real exports, plus unchanged caller facts.
			require.Equal(t, sentinel, original.response)
			require.Equal(t, tc.businessErr, original.err)
			spans := f.spans.GetSpans()
			require.Len(t, spans, 1)
			s := spans[0]
			require.Equal(t, tc.status, s.Status.Code)
			require.Equal(t, tc.state, value(t, s.Attributes, "host.model.provider_outcome").AsString())
			require.Equal(t, attribute.BOOL, value(t, s.Attributes, "host.model.domain_success").Type())
			require.False(t, value(t, s.Attributes, "host.model.domain_success").AsBool())
			require.Equal(t, attribute.INT64, value(t, s.Attributes, genai.InputTokens).Type())
			require.Zero(t, value(t, s.Attributes, genai.InputTokens).AsInt64())
			require.Equal(t, int64(7), value(t, s.Attributes, genai.OutputTokens).AsInt64())
			require.Equal(
				t,
				"measured",
				value(t, s.Attributes, "metry.gen_ai.measurement.input_tokens.source").AsString(),
			)
			require.Equal(t, "token", value(t, s.Attributes, "metry.gen_ai.measurement.input_tokens.unit").AsString())
			if tc.class == "" {
				absent(t, s.Attributes, genai.ErrorType)
			} else {
				require.Equal(t, tc.class, value(t, s.Attributes, genai.ErrorType).AsString())
			}
			require.NotContains(t, fmt.Sprint(s), sentinel)
			assertTokens(t, f.metrics(t), genai.OutputTokenUsageMetricName, 7, 1)
		})
	}
	// Arrange / Act / Assert: unsupported states reject before export, including unsafe text.
	f := newFixture(t)
	_, err := mapModel(modelResult{state: sentinel})
	require.ErrorIs(t, err, errMapping)
	require.NotContains(t, err.Error(), sentinel)
	require.Empty(t, f.spans.GetSpans())
}

func TestUsagePresenceComponentsAndSequence(t *testing.T) {
	// Arrange: unknown, measured zero, partial, and inclusive totals with overlapping components.
	f := newFixture(t)
	samples := []hostUsage{
		{}, {input: reported(0, "tokens", "observed")}, {output: reported(3, "tokens", "estimated")},
		{input: reported(10, "tokens", "reported"), output: reported(8, "tokens", "reported"),
			cacheRead: reported(6, "tokens", "reported"), cacheWrite: reported(6, "tokens", "reported"),
			reasoning: reported(5, "tokens", "reported"), cost: reported(0.25, "money", "reported"), currency: "USD"},
	}
	// Act.
	for _, u := range samples {
		mapped, err := mapModel(modelResult{state: "completed", usage: u})
		require.NoError(t, err)
		require.NoError(
			t,
			f.tracker.Recorder().
				RecordOperation(t.Context(), genai.Operation{Name: "chat"}, mapped.result, mapped.attributes...),
		)
	}
	// Assert: unknown produces no value, partial does not reconstruct totals, components are separate.
	spans := f.spans.GetSpans()
	require.Len(t, spans, 4)
	absent(t, spans[0].Attributes, genai.InputTokens)
	absent(t, spans[0].Attributes, genai.OutputTokens)
	absent(t, spans[2].Attributes, genai.InputTokens)
	absent(t, spans[2].Attributes, genai.UsageCost)
	data := f.metrics(t)
	assertTokens(t, data, genai.InputTokenUsageMetricName, 10, 2)
	assertTokens(t, data, genai.OutputTokenUsageMetricName, 11, 2)
	assertTokens(t, data, genai.TokenComponentUsageMetricName, 17, 3)
	require.InDelta(
		t,
		0.25,
		metrytest.Float64SumValue(t, data, metrytest.MetricSelector{Name: genai.CostMetricName}),
		1e-9,
	)

	for _, tc := range []struct {
		name    string
		updates []usageUpdate
		want    int
		reject  bool
	}{
		{"delta", []usageUpdate{{1, "delta", 2, false}, {2, "delta", 3, true}}, 5, false},
		{"zero", []usageUpdate{{1, "delta", 0, true}}, 0, false},
		{"repeated snapshot", []usageUpdate{{1, "cumulative", 2, false}, {2, "cumulative", 2, false}, {3, "cumulative", 5, true}}, 5, false},
		{"missing terminal", []usageUpdate{{1, "delta", 2, false}}, 0, true},
		{"duplicate delta", []usageUpdate{{1, "delta", 2, false}, {1, "delta", 2, true}}, 0, true},
		{"out of order", []usageUpdate{{2, "delta", 2, false}, {1, "delta", 2, true}}, 0, true},
		{"gap", []usageUpdate{{1, "delta", 2, false}, {3, "delta", 2, true}}, 0, true},
		{"mixed", []usageUpdate{{1, "delta", 2, false}, {2, "cumulative", 2, true}}, 0, true},
		{"regression", []usageUpdate{{1, "cumulative", 3, false}, {2, "cumulative", 2, true}}, 0, true},
		{"overflow", []usageUpdate{{1, "delta", math.MaxInt, false}, {2, "delta", 1, true}}, 0, true},
		{"negative", []usageUpdate{{1, "delta", -1, true}}, 0, true},
		{"early terminal", []usageUpdate{{1, "delta", 1, true}, {2, "delta", 1, true}}, 0, true},
		{"empty", nil, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange / Act.
			usage, err := normalizeUpdates(tc.updates)
			// Assert.
			if tc.reject {
				require.ErrorIs(t, err, errMapping)
				require.Equal(t, hostUsage{}, usage)
			} else {
				require.NoError(t, err)
				require.True(t, usage.output.known)
				require.Equal(t, tc.want, usage.output.value)
			}
		})
	}
}

func TestAttemptOwnershipAndConcurrentTerminal(t *testing.T) {
	// Arrange: two physical attempts, a logical summary and a host step.
	f := newFixture(t)
	require.ErrorIs(t, checkOwner([]string{"producer", "observer"}), errMapping)
	require.ErrorIs(t, checkOwner(nil), errMapping)
	require.Empty(t, f.spans.GetSpans())
	require.NoError(t, checkOwner([]string{"model"}))
	_, logical, err := f.tracker.Recorder().
		StartOperation(t.Context(), genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{Identity: genai.CausalIdentity{RunID: "run", OperationID: "invocation", StepID: "step", Kind: genai.IdentityLogical}})
	require.NoError(t, err)
	for attempt := range 2 {
		_, h, startErr := f.tracker.Recorder().
			StartOperation(t.Context(), genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{Identity: genai.CausalIdentity{RunID: "run", OperationID: "invocation", StepID: "step", AttemptID: strconv.Itoa(attempt), Kind: genai.IdentityAttempt}})
		require.NoError(t, startErr)
		// Act: invalid terminal is correctable, then racing callers submit the accepted terminal.
		require.Error(
			t,
			h.Finish(
				genai.OperationResult{
					Usage: genai.Usage{OutputTokens: genai.Observe(-1, genai.UnitTokens, genai.SourceProvider)},
				},
			),
		)
		require.Empty(t, f.spans.GetSpans())
		result := genai.OperationResult{
			Status: genai.OperationStatusOK,
			Usage:  genai.Usage{OutputTokens: genai.Observe(5, genai.UnitTokens, genai.SourceProvider)},
		}
		if attempt == 0 {
			result.Status, result.ErrorType = genai.OperationStatusError, "unavailable"
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 16)
		for range 16 {
			wg.Go(func() { outcomes <- h.Finish(result) })
		}
		wg.Wait()
		close(outcomes)
		successes := 0
		for outcome := range outcomes {
			if outcome == nil {
				successes++
			} else {
				require.ErrorIs(t, outcome, genai.ErrFinalized)
			}
		}
		require.Equal(t, 1, successes)
		// Only ended attempts are visible; reset the trace exporter between attempts for rejection evidence.
		require.Len(t, f.spans.GetSpans(), 1)
		f.spans.Reset()
	}
	require.NoError(
		t,
		logical.Finish(
			genai.OperationResult{
				Status: genai.OperationStatusOK,
				Usage:  genai.Usage{OutputTokens: genai.Observe(10, genai.UnitTokens, genai.SourceProvider)},
			},
		),
	)
	_, endStep, err := f.provider.StartSpan(t.Context(), "host", "step")
	require.NoError(t, err)
	endStep()
	// Assert: both physical attempts account, neither logical summary nor step repeats it.
	assertTokens(t, f.metrics(t), genai.OutputTokenUsageMetricName, 10, 2)
}

func TestRouteReturnAndAcceptedTerminal(t *testing.T) {
	for _, first := range []bool{false, true} {
		t.Run(strconv.FormatBool(first), func(t *testing.T) {
			// Arrange: handler starts a model stream then returns its handle to the host.
			f := newFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			_, h, err := f.tracker.Recorder().
				StartOperation(ctx, genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{})
			require.NoError(t, err)
			// Act: intermediate usage and handler return do not finalize anything.
			require.NoError(
				t,
				h.ValidateStreamingUpdate(
					genai.Usage{OutputTokens: genai.Observe(99, genai.UnitTokens, genai.SourceProvider)},
				),
			)
			cancel()
			require.Empty(t, f.spans.GetSpans())
			metrytest.AssertMetricAbsent(
				t,
				f.metrics(t),
				metrytest.MetricSelector{Name: genai.OutputTokenUsageMetricName},
			)
			mapped, err := mapModel(modelResult{state: "cancelled", err: context.Canceled})
			require.NoError(t, err)
			mapped.result.Streaming = &genai.StreamTimings{FirstTokenPresent: first}
			require.NoError(t, h.Finish(mapped.result, mapped.attributes...))
			// Assert: accepted terminal preserves evidence without inferring final tokens.
			spans := f.spans.GetSpans()
			require.Len(t, spans, 1)
			require.Equal(t, codes.Error, spans[0].Status.Code)
			require.Equal(t, "cancelled", value(t, spans[0].Attributes, "host.model.provider_outcome").AsString())
			absent(t, spans[0].Attributes, genai.OutputTokens)
			data := f.metrics(t)
			metrytest.AssertMetricAbsent(t, data, metrytest.MetricSelector{Name: genai.OutputTokenUsageMetricName})
			if first {
				h := metrytest.FindFloat64Histogram(t, data, metrytest.MetricSelector{Name: genai.TTFTMetricName})
				require.Len(t, h.DataPoints, 1)
				require.Equal(t, uint64(1), h.DataPoints[0].Count)
				require.Zero(t, h.DataPoints[0].Sum)
			} else {
				metrytest.AssertMetricAbsent(t, data, metrytest.MetricSelector{Name: genai.TTFTMetricName})
			}
		})
	}
}

func TestMappedEvaluationsExportAndDelivery(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(strconv.FormatBool(raw), func(t *testing.T) {
			// Arrange: host-origin reference and ready assessment with explicit association.
			opts := []genai.Option{genai.WithPayloadPolicy(genai.RedactPayloadPolicy())}
			if raw {
				opts = []genai.Option{genai.WithRawPayloads()}
			}
			f := newFixture(t, opts...)
			originCtx, origin := f.sdk.Tracer("host", trace.WithSchemaURL("https://host.invalid/schema")).
				Start(t.Context(), "target")
			ref, refErr := metry.NewAsyncHandle(originCtx)
			require.NoError(t, refErr)
			origin.End()
			cases := []struct {
				state, label string
				score        bool
				status       string
			}{
				{"scored", "", true, "succeeded"},
				{"assertion", "false", false, "succeeded"},
				{
					"unavailable",
					"",
					false,
					"failed",
				},
				{"grader_error", "", false, "failed"},
				{"skipped", "", false, "skipped"},
			}
			for i, tc := range cases {
				input := readyAssessment{
					id:        fmt.Sprintf("obs/%d", i),
					metric:    "faithfulness",
					state:     tc.state,
					reasoning: sentinel,
					grader:    sentinel,
					target:    "independent-target",
					association: hostAssociation{
						experiment: "experiment",
						dataset:    "dataset",
						caseID:     "case",
						trial:      strconv.Itoa(i),
					},
				}
				var link *metry.AsyncHandle
				if i%2 == 0 {
					link = &ref
				}
				// Act: actual adapter -> recorder -> exporter.
				e, err := mapAssessment(input, link)
				require.NoError(t, err)
				require.NoError(t, f.tracker.EvaluationRecorder().Record(originCtx, e))
				assertEvaluationExport(
					t,
					f.spans.GetSpans()[i+1],
					input,
					link,
					origin.SpanContext(),
					raw,
					tc.status,
					tc.label,
					tc.score,
					i,
				)
			}
			// Independent grader-only references and no experiment are supported.
			e, err := mapAssessment(
				readyAssessment{id: "standalone", metric: "answer_relevance", state: "scored", grader: "grader-only"},
				nil,
			)
			require.NoError(t, err)
			require.NoError(t, f.tracker.EvaluationRecorder().Record(t.Context(), e))
			s := f.spans.GetSpans()[6]
			absent(t, s.Attributes, "metry.evaluation.target_reference")
			absent(t, s.Attributes, "metry.evaluation.experiment_id")
			// Repeated telemetry recording has repeated observations, not hidden dedup.
			require.NoError(t, f.tracker.EvaluationRecorder().Record(t.Context(), e))
			repeated := f.spans.GetSpans()[7]
			require.NotEqual(t, s.SpanContext.SpanID(), repeated.SpanContext.SpanID())
			require.Equal(
				t,
				value(t, s.Attributes, "metry.evaluation.observation_id"),
				value(t, repeated.Attributes, "metry.evaluation.observation_id"),
			)
			// Host sink: commit succeeds but acknowledgement is lost; replay retains one measurement.
			var sink measurementSink
			require.Error(t, sink.accept(e, true))
			require.NoError(t, sink.accept(e, false))
			require.Len(t, sink.measurements, 1)
			changed := e
			changed.Metric = genai.EvaluationFaithfulness
			changed.ObservationID = "other-metric"
			require.NoError(t, sink.accept(changed, false))
			changed.ObservationID = "other-trial"
			changed.Association = genai.EvaluationAssociation{
				ExperimentID:     "experiment",
				DatasetReference: "dataset",
				CaseID:           "case",
				TrialID:          "trial-2",
			}
			require.NoError(t, sink.accept(changed, false))
			require.Equal(t, genai.EvaluationFaithfulness, sink.measurements["other-metric"].Metric)
			require.Equal(t, "trial-2", sink.measurements["other-trial"].Association.TrialID)
			require.NoError(t, f.tracker.EvaluationRecorder().Record(t.Context(), sink.measurements["other-metric"]))
			require.NoError(t, f.tracker.EvaluationRecorder().Record(t.Context(), sink.measurements["other-trial"]))
			metricSpan, trialSpan := f.spans.GetSpans()[8], f.spans.GetSpans()[9]
			require.Equal(t, "faithfulness", value(t, metricSpan.Attributes, genai.EvaluationMetricName).AsString())
			trialValue := "trial-2"
			if !raw {
				trialValue = fingerprint(trialValue)
			}
			require.Equal(t, trialValue, value(t, trialSpan.Attributes, "metry.evaluation.trial_id").AsString())
			require.Len(t, sink.measurements, 3)
			changed.ObservationID = e.ObservationID
			changed.Score = nil
			changed.Outcome = "false"
			require.ErrorIs(t, sink.accept(changed, false), errMapping)
		})
	}
	// Arrange / Act / Assert: unknown state and partial tuple reject with no export.
	f := newFixture(t)
	for _, input := range []readyAssessment{{id: "bad", metric: "faithfulness", state: sentinel}, {id: "bad", metric: "faithfulness", state: "scored", association: hostAssociation{caseID: "case"}}} {
		_, err := mapAssessment(input, nil)
		require.Error(t, err)
	}
	require.Empty(t, f.spans.GetSpans())
}

func TestIndependentCaptureAndForeignSpan(t *testing.T) {
	// Arrange: separate sinks each have an explicit policy; shared SDK includes a foreign span.
	globalTrace, globalMeter, globalPropagator := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
	redact := newFixture(t, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
	hash := newFixture(t, genai.WithPayloadPolicy(genai.HashPayloadPolicy()))
	require.Same(t, globalTrace, otel.GetTracerProvider())
	require.Same(t, globalMeter, otel.GetMeterProvider())
	require.Equal(t, globalPropagator, otel.GetTextMapPropagator())
	ctx, foreign := redact.sdk.Tracer("host", trace.WithSchemaURL("https://host.invalid/schema")).
		Start(t.Context(), "foreign")
	foreign.SetAttributes(attribute.String("host.unprotected", sentinel))
	mapped, err := mapModel(modelResult{state: "completed", response: sentinel, err: errors.New(sentinel)})
	require.NoError(t, err)
	// Act.
	for _, f := range []fixture{redact, hash} {
		require.NoError(
			t,
			f.tracker.Recorder().
				RecordOperation(ctx, genai.Operation{Name: "chat", Model: sentinel}, mapped.result, mapped.attributes...),
		)
		require.NoError(t, f.provider.Shutdown(t.Context()))
		require.NoError(t, f.provider.ForceFlush(t.Context()))
		require.NoError(t, f.provider.FlushDependencies(t.Context()))
	}
	foreign.End()
	// Assert: own channels are safe, and privacy never rewrites a foreign span.
	own := redact.spans.GetSpans()[0]
	require.Equal(t, foreign.SpanContext().TraceID(), own.SpanContext.TraceID())
	require.Equal(t, foreign.SpanContext().SpanID(), own.Parent.SpanID())
	require.NotContains(t, fmt.Sprint(own), sentinel)
	require.Contains(t, value(t, own.Attributes, genai.OutputMessages).AsString(), "[redacted]")
	require.Contains(t, value(t, hash.spans.GetSpans()[0].Attributes, genai.OutputMessages).AsString(), "sha256:")
	foreignExport := redact.spans.GetSpans()[1]
	require.Equal(t, sentinel, value(t, foreignExport.Attributes, "host.unprotected").AsString())
	require.Equal(t, "https://host.invalid/schema", foreignExport.InstrumentationScope.SchemaURL)
	require.Same(t, globalTrace, otel.GetTracerProvider())
	require.Same(t, globalMeter, otel.GetMeterProvider())
	require.Equal(t, globalPropagator, otel.GetTextMapPropagator())
	// The host closes the SDK; borrowed shutdown left it usable.
	_, next := redact.sdk.Tracer("host").Start(t.Context(), "still-owned")
	next.End()
	require.Len(t, redact.spans.GetSpans(), 3)
	// Existing sdk_contract_test covers exact counts of implicit/explicit lifecycle calls.
}

func assertEvaluationExport(
	t *testing.T,
	span tracetest.SpanStub,
	input readyAssessment,
	link *metry.AsyncHandle,
	origin trace.SpanContext,
	raw bool,
	status, labelResult string,
	scored bool,
	i int,
) {
	t.Helper()
	// Assert: scope/schema, new root/link, event and span result presence.
	s := span
	require.False(t, s.Parent.IsValid())
	require.Equal(t, "metry/genai", s.InstrumentationScope.Name)
	require.Equal(t, "https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev", s.InstrumentationScope.SchemaURL)
	require.Len(t, s.Events, 1)
	require.Equal(t, "gen_ai.evaluation.result", s.Events[0].Name)
	if link != nil {
		require.Len(t, s.Links, 1)
		require.Equal(t, origin, s.Links[0].SpanContext)
	} else {
		require.Empty(t, s.Links)
	}
	for _, attrs := range [][]attribute.KeyValue{s.Attributes, s.Events[0].Attributes} {
		assertEvaluationFields(t, attrs, input, raw, status, labelResult, scored, i)
	}
	if !raw {
		require.NotContains(t, fmt.Sprint(s), sentinel)
	}
}

func assertEvaluationFields(
	t *testing.T,
	attrs []attribute.KeyValue,
	input readyAssessment,
	raw bool,
	status, labelResult string,
	scored bool,
	i int,
) {
	t.Helper()
	require.Equal(t, status, value(t, attrs, "metry.evaluation.status").AsString())
	wantID, wantGrader, wantTarget := fingerprint(input.id), fingerprint(sentinel), fingerprint(input.target)
	if raw {
		wantID, wantGrader, wantTarget = input.id, sentinel, input.target
	}
	require.Equal(t, wantID, value(t, attrs, "metry.evaluation.observation_id").AsString())
	require.Equal(t, wantGrader, value(t, attrs, "metry.evaluation.grader_reference").AsString())
	require.Equal(t, wantTarget, value(t, attrs, "metry.evaluation.target_reference").AsString())
	for key, text := range map[string]string{"experiment_id": "experiment", "dataset_reference": "dataset", "case_id": "case", "trial_id": strconv.Itoa(i)} {
		if !raw {
			text = fingerprint(text)
		}
		require.Equal(t, text, value(t, attrs, "metry.evaluation."+key).AsString())
	}
	if scored {
		require.Equal(t, attribute.FLOAT64, value(t, attrs, genai.EvaluationScore).Type())
		require.Zero(t, value(t, attrs, genai.EvaluationScore).AsFloat64())
	} else {
		absent(t, attrs, genai.EvaluationScore)
	}
	if labelResult != "" {
		label := labelResult
		if !raw {
			label = fingerprint(label)
		}
		require.Equal(t, label, value(t, attrs, genai.EvaluationLabel).AsString())
	} else {
		absent(t, attrs, genai.EvaluationLabel)
	}
}

func TestTerminalAttributesBelongToAcceptedResult(t *testing.T) {
	// Arrange: different business outcomes race for the same physical interval.
	f := newFixture(t)
	_, h, err := f.tracker.Recorder().
		StartOperation(t.Context(), genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{})
	require.NoError(t, err)
	invalid := genai.OperationResult{
		Usage: genai.Usage{OutputTokens: genai.Observe(-1, genai.UnitTokens, genai.SourceProvider)},
	}
	require.Error(t, h.Finish(invalid, metry.StringAttribute("host.rejected", sentinel)))
	domainFalse, domainTrue := false, true
	inputs := []modelResult{
		{state: "completed", domainSuccess: &domainTrue, usage: hostUsage{output: reported(3, "tokens", "reported")}},
		{state: "cancelled", domainSuccess: &domainFalse, usage: hostUsage{output: reported(7, "tokens", "reported")}},
	}
	accepted := make(chan int, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	// Act: metadata is passed into Finish, not written onto the span beforehand.
	for i, input := range inputs {
		mapped, mapErr := mapModel(input)
		require.NoError(t, mapErr)
		wg.Go(func() {
			if finishErr := h.Finish(mapped.result, mapped.attributes...); finishErr == nil {
				accepted <- i
			} else {
				failures <- finishErr
			}
		})
	}
	wg.Wait()
	close(accepted)
	close(failures)
	// Assert: exactly one result and its attributes, never the rejected caller's facts.
	require.Len(t, accepted, 1)
	require.Len(t, failures, 1)
	winner := <-accepted
	require.ErrorIs(t, <-failures, genai.ErrFinalized)
	require.ErrorIs(
		t,
		h.Finish(genai.OperationResult{}, metry.StringAttribute("host.rejected", sentinel)),
		genai.ErrFinalized,
	)
	s := f.spans.GetSpans()[0]
	require.Equal(t, inputs[winner].state, value(t, s.Attributes, "host.model.provider_outcome").AsString())
	require.Equal(t, *inputs[winner].domainSuccess, value(t, s.Attributes, "host.model.domain_success").AsBool())
	absent(t, s.Attributes, "host.rejected")
	wantStatus := codes.Ok
	if winner == 1 {
		wantStatus = codes.Error
	}
	require.Equal(t, wantStatus, s.Status.Code)
	assertTokens(t, f.metrics(t), genai.OutputTokenUsageMetricName, int64(inputs[winner].usage.output.value), 1)
}

func TestHostMeasurementMappingRejectsContradictions(t *testing.T) {
	for _, u := range []hostUsage{
		{input: reading[int]{value: 1}}, {input: reported(1, "unknown-unit", "reported")},
		{input: reported(1, "tokens", "unknown-source")}, {input: reported(1, "money", "observed")},
		{input: reported(-1, "tokens", "reported")}, {cost: reported(math.NaN(), "money", "reported"), currency: "USD"},
	} {
		// Arrange / Act / Assert: unsupported provenance never becomes a guess.
		_, err := mapModel(modelResult{state: "completed", usage: u})
		require.Error(t, err)
	}
}
