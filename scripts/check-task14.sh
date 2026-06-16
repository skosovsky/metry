#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GREP_EXCLUDE=(--exclude-dir=.git --exclude-dir=vendor --exclude-dir=node_modules)

fail() {
	echo "check-task14: $1" >&2
	exit 1
}

grep_go() {
	grep -rE "$@" --include='*.go' "${GREP_EXCLUDE[@]}" .
}

grep_go_match() {
	grep_go "$@" >/dev/null 2>&1
}

if [ -d traceutil ]; then
	fail 'found public traceutil directory on disk'
fi

if git ls-files 'traceutil/*.go' 2>/dev/null | grep -q .; then
	fail 'found tracked public traceutil package in git'
fi

if grep_go_match 'context\.WithValue|OtelProvider\(\)|Provider\.Tracer\(|Provider\.Meter\('; then
	fail 'found forbidden context/Provider OTel accessors'
fi

if grep_go 'func \([^)]*\) [A-Z][A-Za-z0-9]*OnSpan\(' genai/ 2>/dev/null | grep -v '_test.go' >/dev/null; then
	fail 'found public genai OnSpan API'
fi

if grep_go_match 'InjectToJSON|ExtractFromJSON|SetBaggageValue|NewTrackerWithTracer|RecordSecurityEvent\('; then
	fail 'found removed legacy API'
fi

if grep -r 'github.com/skosovsky/metry/traceutil' --include='*.go' "${GREP_EXCLUDE[@]}" . >/dev/null 2>&1; then
	fail 'found public traceutil import'
fi

if grep_go 'func NewProviderFromDeps' | grep -v 'internal/' | grep -v 'metrytest/' >/dev/null 2>&1; then
	fail 'found public NewProviderFromDeps outside metrytest'
fi

if grep_go '^func (UnwrapTraceSampler|WrapTraceSampler|SpanExporterFromSDK|MetricExporterFromSDK|TraceSamplerFromSDK|SDKSampler)' \
	| grep -v 'internal/' | grep -v 'metrytest/' | grep -v 'wire_init.go' | grep -v '_test.go' >/dev/null 2>&1; then
	fail 'found public SDK escape hatches outside tests'
fi

if grep_go_match 'func.*GenAIDeps|type GenAIDeps'; then
	fail 'found public GenAIDeps'
fi

if grep -r '^package traceutil' --include='*.go' "${GREP_EXCLUDE[@]}" . | grep -v '/internal/' >/dev/null 2>&1; then
	fail 'found public traceutil package'
fi

if grep -rE 'sdktrace\.NeverSample' --include='*.go' genai/ 2>/dev/null | grep -v '_test.go' | grep -v '/internal/' >/dev/null 2>&1; then
	fail 'found sdktrace.NeverSample in public genai'
fi

if grep_go_match 'WithLinked(Bool|String|Float|Int)Attribute|WithSpanBoolAttribute|linkedspan\.With(Bool|String|Float|Int)Attribute'; then
	fail 'found string-key span attribute helpers (use typed metry.Attribute)'
fi

if grep -rE 'executor\.operation\.' README.md docs/ .cursor/docs/task12.md >/dev/null 2>&1; then
	fail 'found stale executor.operation.* metric names in docs'
fi

if grep -rE 'sdktrace\.NewTracerProvider|tracetest\.NewInMemoryExporter' --include='*_test.go' "${GREP_EXCLUDE[@]}" . \
	| grep -v 'testutil/' | grep -v 'internal/async/' | grep -v 'propagation/' | grep -v 'propagation_provider_test.go' >/dev/null 2>&1; then
	fail 'found raw SDK test setup outside allowed white-box tests'
fi

if grep -l 'internal/otelbridge' *_test.go 2>/dev/null | grep -qv 'export'; then
	fail 'found otelbridge import in package metry white-box tests'
fi

if grep -r 'tracerProvider()' --include='*_test.go' "${GREP_EXCLUDE[@]}" . \
	| grep -v '/internal/' >/dev/null 2>&1; then
	fail 'found tracerProvider() bypass in tests'
fi

if grep -rE 'NewTrackerWithTracer|genai\.NewTracker\(' .cursor/docs/task8.md .cursor/docs/task9.md >/dev/null 2>&1; then
	fail 'found stale genai.NewTracker API in task8/task9 docs'
fi

if grep_go 'func \(.*Attribute\) ToOTel\(' | grep -v 'internal/' >/dev/null 2>&1; then
	fail 'found public Attribute.ToOTel outside internal/'
fi

if grep -r 'internal/linkedspan' --include='*_test.go' genai/ >/dev/null 2>&1; then
	fail 'found internal/linkedspan import in genai tests'
fi

if grep -r 'go.opentelemetry.io/otel/attribute' --include='*_test.go' genai/ >/dev/null 2>&1; then
	fail 'found otel/attribute import in genai tests'
fi

if grep -rE 'RecordSecurityEvent\(' .cursor/docs/task2.md .cursor/docs/task10.md .cursor/docs/task11.md 2>/dev/null \
	| grep -v 'RecordSecurityEventWithProvider' >/dev/null 2>&1; then
	fail 'found stale RecordSecurityEvent in task2/task10/task11 docs'
fi

if grep -r 'SetBaggageValue' .cursor/docs/task5.md 2>/dev/null | grep -v 'Superseded' | grep -v 'superseded' | grep -v 'удалён' >/dev/null 2>&1; then
	fail 'found stale SetBaggageValue in task5 docs'
fi

if grep -rE 'trace\.SpanStartOption' .cursor/docs/task10.md >/dev/null 2>&1; then
	fail 'found stale trace.SpanStartOption in task10 docs'
fi

if grep -rE 'trace\.SpanFromContext' .cursor/docs/task2.md >/dev/null 2>&1; then
	fail 'found stale trace.SpanFromContext in task2 docs'
fi

if grep -rE 'trace\.WithAttributes' .cursor/docs/task3.md >/dev/null 2>&1; then
	fail 'found stale trace.WithAttributes in task3 docs'
fi

if grep -rE 'go\.opentelemetry\.io/otel/(trace|baggage|codes|metric)' --include='*_test.go' genai/ >/dev/null 2>&1; then
	fail 'found raw OTel imports in genai tests'
fi

for f in *_test.go; do
	[ "$f" = "metry_export_test.go" ] && continue
	grep -q '^package metry_test' "$f" || continue
	if grep -qE 'go\.opentelemetry\.io/otel/' "$f"; then
		fail "found raw OTel imports in package metry_test file: $f"
	fi
done

for f in task14_e2e_test.go task14_scope_e2e_test.go \
	task14_async_feedback_e2e_test.go task14_metrics_e2e_test.go \
	task14_evaluations_e2e_test.go; do
	if [ ! -f "$f" ]; then
		fail "missing required E2E test file: $f"
	fi
done

if ! grep -q 'RecordEvaluations' task14_evaluations_e2e_test.go 2>/dev/null; then
	fail 'task14_evaluations_e2e_test.go must exercise RecordEvaluations'
fi

if ! grep -q 'InjectToMap' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must exercise InjectToMap worker context'
fi

if ! grep -q 'TestRecordInteraction_WithScopeModelOnly_SkipsTokenMetrics' genai/metrics_test.go 2>/dev/null; then
	fail 'genai/metrics_test.go must include scope metrics footgun regression'
fi

if ! grep -rq 'TestRecordInteraction_ErrorType' genai/*_test.go 2>/dev/null; then
	fail 'genai tests must include ErrorType span status regression'
fi

if grep -r 'tracetest\.SpanStub' --include='*.go' "${GREP_EXCLUDE[@]}" . \
	| grep -v '/internal/' | grep -v '/testutil/' | grep -v 'metrytest/' >/dev/null 2>&1; then
	fail 'found tracetest.SpanStub outside internal/testutil/metrytest'
fi

if [ -d traceutil ]; then
	fail 'found public traceutil directory on disk'
fi

if ! grep -q 'NewMetricsRegistry' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must exercise NewMetricsRegistry'
fi

if ! diff -q docs/task14-migration.md .cursor/docs/task14-migration.md >/dev/null 2>&1; then
	fail 'docs/task14-migration.md and .cursor/docs/task14-migration.md differ'
fi

if ! grep -rq 'TestRecordOperation_Success_SetsOkStatus' genai/*_test.go 2>/dev/null; then
	fail 'genai tests must include RecordOperation success SpanOK regression'
fi

if ! grep -q 'OperationPurpose' task14_scope_e2e_test.go 2>/dev/null; then
	fail 'task14_scope_e2e_test.go must assert OperationPurpose on interaction span'
fi

if ! grep -rq 'TestRunLinkedSpan_Success_SetsOkStatus' internal/async/*_test.go 2>/dev/null; then
	fail 'internal/async tests must include RunLinkedSpan success SpanOK regression'
fi

if ! grep -rq 'TestRecordInteraction_Success_SetsOkStatus' genai/*_test.go 2>/dev/null; then
	fail 'genai tests must include RecordInteraction success SpanOK regression'
fi

if ! grep -q 'MutateRecordingSpan' internal/traceutil/span.go 2>/dev/null; then
	fail 'internal/traceutil must export MutateRecordingSpan'
fi

if ! grep -q 'mergeMetaFromScopeWithScope' genai/scope_context.go 2>/dev/null; then
	fail 'genai/scope_context.go must define mergeMetaFromScopeWithScope'
fi

if grep -r '^package traceutil' --include='*.go' "${GREP_EXCLUDE[@]}" . | grep -v '/internal/' >/dev/null 2>&1; then
	fail 'found public traceutil package outside internal/'
fi

if ! grep -q 'InjectToMap' task14_async_feedback_e2e_test.go 2>/dev/null; then
	fail 'task14_async_feedback_e2e_test.go must exercise InjectToMap worker context'
fi

if ! grep -q 'InjectToMap' task14_evaluations_e2e_test.go 2>/dev/null; then
	fail 'task14_evaluations_e2e_test.go must exercise InjectToMap worker context'
fi

if grep -rE 'trace\.WithAttributes' .cursor/docs/task4.md >/dev/null 2>&1; then
	fail 'found stale trace.WithAttributes in task4 docs'
fi

if grep -rE 'metric\.WithAttributes' .cursor/docs/task4.md .cursor/docs/task7-1.md >/dev/null 2>&1; then
	fail 'found stale metric.WithAttributes in task4/task7-1 docs'
fi

if grep -r 'go.opentelemetry.io/otel/' --include='*_blackbox_test.go' propagation/ >/dev/null 2>&1; then
	fail 'found raw OTel imports in propagation blackbox tests'
fi

if grep -q 'mergeMetaFromScope(' genai/helpers.go 2>/dev/null; then
	fail 'genai/helpers.go must not re-merge scope in recordUsageMetrics/recordOperationDuration'
fi

for f in task14_e2e_test.go task14_async_feedback_e2e_test.go task14_evaluations_e2e_test.go task14_scope_e2e_test.go; do
	if ! grep -q 'AssertSpanStubOkStatus' "$f" 2>/dev/null; then
		fail "$f must assert span Ok status"
	fi
done

if ! grep -q 'ExtractFromMap' examples/metrics_registry/main.go 2>/dev/null; then
	fail 'examples/metrics_registry/main.go must exercise ExtractFromMap worker context'
fi

if ! grep -q 'SpanOKIfUnset' internal/traceutil/span.go 2>/dev/null; then
	fail 'internal/traceutil must export SpanOKIfUnset'
fi

if ! grep -q 'EndSpanOKIfUnset' provider_span.go 2>/dev/null; then
	fail 'provider_span.go must use EndSpanOKIfUnset'
fi

if ! grep -q 'MutateRecordingSpan' security/events.go 2>/dev/null; then
	fail 'security/events.go must guard AddEvent with MutateRecordingSpan'
fi

if ! grep -q 'func (r \*MetricsRegistry) NewCounter' metrics_registry.go 2>/dev/null; then
	fail 'metrics_registry.go must export NewCounter (G21)'
fi

if ! grep -q 'func (r \*MetricsRegistry) NewGauge' metrics_registry.go 2>/dev/null; then
	fail 'metrics_registry.go must export NewGauge (G22)'
fi

if ! grep -q 'NewCounter' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must exercise NewCounter (G23)'
fi

if ! grep -q 'agent_loop_steps' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must exercise agent_loop_steps counter (G24)'
fi

if ! grep -q 'AssertSpanStubOkStatus' async_handle_test.go 2>/dev/null \
	|| ! grep -q 'RecordLinkedOutcome' async_handle_test.go 2>/dev/null; then
	fail 'async_handle_test.go must assert SpanOK on RecordLinkedOutcome paths (G25)'
fi

if ! grep -q 'v == ""' labels.go 2>/dev/null; then
	fail 'labels.go must skip empty label values (G26)'
fi

if ! grep -q 'InjectToMap' examples/scope/main.go 2>/dev/null; then
	fail 'examples/scope/main.go must exercise InjectToMap worker flow (G27)'
fi

if ! grep -q 'NewGauge' task14_metrics_e2e_test.go 2>/dev/null \
	|| ! grep -q 'queue_depth' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must exercise NewGauge queue_depth (G28)'
fi

if ! grep -q 'NewGauge' examples/metrics_registry/main.go 2>/dev/null \
	|| ! grep -q 'queue_depth' examples/metrics_registry/main.go 2>/dev/null; then
	fail 'examples/metrics_registry/main.go must exercise NewGauge queue_depth (G29)'
fi

if ! grep -q 'TestCopyLabels_SkipsEmptyKeyAndValue' labels_test.go 2>/dev/null; then
	fail 'labels_test.go must include TestCopyLabels_SkipsEmptyKeyAndValue (G30)'
fi

if ! grep -q 'TestEnrich_WithoutActiveSpan_NoSpanExport' enrich_test.go 2>/dev/null; then
	fail 'enrich_test.go must include TestEnrich_WithoutActiveSpan_NoSpanExport (G31)'
fi

if grep -E 'RecordSecurityEvent[^W]|RecordSecurityEvent`' .cursor/docs/task2.md 2>/dev/null \
	| grep -v RecordSecurityEventWithProvider | grep -q .; then
	fail 'task2.md must not reference stale RecordSecurityEvent API (G32)'
fi

if ! grep -q 'TestLabelsToAttributes_SkipsEmptyKeyAndValue' metrics_registry_test.go 2>/dev/null; then
	fail 'metrics_registry_test.go must include TestLabelsToAttributes_SkipsEmptyKeyAndValue (G33)'
fi

if ! grep -q 'Int64SumValue' task14_metrics_e2e_test.go 2>/dev/null \
	|| ! grep -q 'GaugeFloat64Value' task14_metrics_e2e_test.go 2>/dev/null; then
	fail 'task14_metrics_e2e_test.go must assert metric values via metrytest helpers (G34)'
fi

if ! grep -q 'func FindInt64Sum' metrytest/metricassert.go 2>/dev/null \
	|| ! grep -q 'func FindGauge' metrytest/metricassert.go 2>/dev/null; then
	fail 'metrytest/metricassert.go must export FindInt64Sum and FindGauge (G35)'
fi

echo "check-task14: all gates passed"
