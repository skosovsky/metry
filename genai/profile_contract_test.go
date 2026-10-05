package genai

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestProfileRejectsUnsafeDeclarationsBeforeFixtures(t *testing.T) {
	for _, scenario := range []string{"unsupported_profile", "raw", "ownership", "mapping", "unit", "fields", "missing_field", "unknown_field", "encoding", "boundary_missing", "boundary_accounting", "boundary_operation", "boundary_unknown"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			profile := DefaultProfile()
			adapter := AdapterDeclaration{
				ProfileID:  profile.ID,
				Mapping:    profile.Mapping,
				Boundaries: []string{"model"},
				Units:      []FieldUnitDeclaration{{Field: InputTokens, Unit: UnitTokens}},
			}
			declarations := []AdapterDeclaration{adapter}
			raw := false
			called := false
			switch scenario {
			case "unsupported_profile":
				profile.ID = "fixture-secret"
			case "raw":
				raw = true
			case "ownership":
				declarations = append(declarations, adapter)
			case "mapping":
				declarations[0].Mapping = "fixture-secret"
			case "unit":
				declarations[0].Units = []FieldUnitDeclaration{{Field: InputTokens, Unit: UnitFrames}}
			case "fields":
				profile.Fields[7].Unit = UnitFrames
			case "missing_field":
				profile.Fields = profile.Fields[:len(profile.Fields)-1]
			case "unknown_field":
				profile.Fields = append(
					profile.Fields,
					ProfileField{Name: "fixture-secret", Encoding: EncodingString, Stability: StabilityExtension},
				)
			case "encoding":
				profile.Fields[len(profile.Fields)-1].Encoding = EncodingInteger
			case "boundary_missing":
				profile.Boundaries = profile.Boundaries[:len(profile.Boundaries)-1]
			case "boundary_accounting":
				profile.Boundaries[0].Accounting = false
			case "boundary_operation":
				profile.Boundaries[0].Operation = "fixture-secret"
			case "boundary_unknown":
				profile.Boundaries = append(
					profile.Boundaries,
					Boundary{Name: "fixture-secret", Operation: "fixture-secret"},
				)
			}
			// Act.
			_, err := CheckConformance(testContract(profile, raw),
				declarations,
				func() ([]ObservedBoundary, error) { called = true; return nil, nil },
			)
			// Assert.
			require.ErrorIs(t, err, ErrInvalidObservation)
			assert.False(t, called)
			assert.NotContains(t, err.Error(), "fixture-secret")
		})
	}
}

func TestProfileSchemaDeclarationsAreDetachedAndCoverOwnedChannels(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	// Act.
	enums := profile.Enumerations()
	channels := profile.PayloadChannels()
	// Assert.
	require.NoError(t, profile.Validate())
	require.LessOrEqual(t, len(profile.Fields), maxProfileEntries)
	require.NotEmpty(t, enums)
	enums[0].Values[0] = "changed"
	assert.Equal(t, string(IdentityLogical), profile.Enumerations()[0].Values[0])
	for _, channel := range channels {
		assert.Equal(t, maxPayloadInputBytes, channel.InputByteLimit)
		assert.NotEmpty(t, channel.BudgetSetting)
		found := false
		for _, field := range profile.Fields {
			if field.Name == channel.Attribute {
				found = field.Encoding == channel.Encoding
			}
		}
		assert.True(t, found, "channel has no matching field: %s", channel.Attribute)
	}
	channels[0].Attribute = "changed"
	assert.Equal(t, SystemInstructions, profile.PayloadChannels()[0].Attribute)
	events := profile.Events()
	for _, event := range events {
		assert.NotEmpty(t, event.Owner)
		assert.NotEmpty(t, event.Fields)
		for _, name := range event.Fields {
			found := false
			for _, field := range profile.Fields {
				found = found || field.Name == name
			}
			assert.True(t, found, "undeclared event field: %s", name)
		}
	}
	events[0].Fields[0] = "changed"
	assert.Equal(t, AgentName, profile.Events()[0].Fields[0])
}

func TestConformanceRejectsObservedBoundaryWithoutDeclaredOwner(t *testing.T) {
	// Arrange: schema compatibility alone does not establish composition ownership.
	profile := DefaultProfile()
	declaration := AdapterDeclaration{
		ProfileID:  profile.ID,
		Mapping:    profile.Mapping,
		Boundaries: []string{ToolMetricLabelTool},
	}
	// Act.
	report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
		func() ([]ObservedBoundary, error) {
			return []ObservedBoundary{{Boundary: profileModelBoundary, Operation: profileChatOperation,
				Fields: []ProfileField{profile.Fields[1]}}}, nil
		})
	// Assert.
	require.NoError(t, err)
	assert.False(t, report.Passed())
	assert.Contains(t, report.Checks, ConformanceCheck{Fixture: 0, Instrument: "", Rule: "ownership",
		Expected: "declared_owner", Actual: "invalid", Passed: false})
}

func TestConformanceReportsSafeMismatches(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping, Boundaries: []string{"model"}}
	// Act.
	report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
		func() ([]ObservedBoundary, error) { return nil, errors.New("fixture-secret") },
		func() ([]ObservedBoundary, error) {
			return []ObservedBoundary{
				{
					Boundary:  "model",
					Operation: "fixture-secret",
					Fields:    profile.Fields,
					Payloads:  []PayloadEvidence{{Attribute: InputMessages, Value: "fixture-secret"}},
					Series:    []InstrumentSeries{{Instrument: TokenUsageMetricName, Count: profile.SeriesBudget + 1}},
				},
			}, nil
		},
	)
	// Assert.
	require.NoError(t, err)
	assert.False(t, report.Passed())
	for _, check := range report.Checks {
		assert.NotContains(t, fmt.Sprint(check), "fixture-secret")
	}
}

func TestProfileCopiesConfiguration(t *testing.T) {
	// Arrange.
	profile := testInstrumentationProfile()
	option := WithProfile(profile)
	profile.Dimensions[0].Values[0] = "changed"
	profile.Fields[0].Name = "changed"
	tracker, _, _, _ := newTestTrackerWithMetrics(t, option)
	// Act.
	profile.Dimensions[0].Values = nil
	// Assert.
	assert.Equal(t, "scope-provider", tracker.cfg.profile.Dimensions[0].Values[0])
	assert.Equal(t, ProviderName, tracker.cfg.profile.Fields[0].Name)
	detached := tracker.Profile()
	detached.Fields[0].Name = "modified"
	assert.Equal(t, ProviderName, tracker.Profile().Fields[0].Name)
}

func TestRemotePolicyIdentityCannotEnableRawFallback(t *testing.T) {
	// Arrange.
	tracker, _, _ := newTestTracker(t, WithPayloadPolicy(HashPayloadPolicy()))
	// Act.
	err := tracker.CheckPolicy("fixture-secret-unknown-policy")
	// Assert.
	require.ErrorIs(t, err, ErrInvalidObservation)
	assert.NotContains(t, err.Error(), "fixture-secret")
	assert.Equal(t, "hash", tracker.Profile().PayloadPolicyID)
	assert.False(t, tracker.Profile().AllowRaw)
	require.NoError(t, tracker.CheckPolicy("hash"))
}

func TestProfilePolicyIdentityMustMatchInstalledBuiltin(t *testing.T) {
	for _, identity := range []string{"hash", profileRawPolicy} {
		t.Run(identity, func(t *testing.T) {
			// Arrange: a declaration alone cannot install or resolve a policy.
			_, provider, _ := newTestTracker(t)
			profile := DefaultProfile()
			profile.PayloadPolicyID = identity
			profile.AllowRaw = true
			// Act.
			tracker, err := NewTrackerFromProvider(provider, WithProfile(profile))
			// Assert.
			require.ErrorIs(t, err, ErrInvalidObservation)
			assert.Nil(t, tracker)
		})
	}
	// Arrange: option order must not let a profile misrepresent the installed policy.
	_, provider, _ := newTestTracker(t)
	// Act.
	tracker, err := NewTrackerFromProvider(
		provider,
		WithPayloadPolicy(HashPayloadPolicy()),
		WithProfile(DefaultProfile()),
	)
	// Assert.
	require.ErrorIs(t, err, ErrInvalidObservation)
	assert.Nil(t, tracker)
}

func TestCardinalityBudgetBoundsConcurrentModelToolSeries(t *testing.T) {
	// Arrange: enough individually allowed values to exceed the combination budget.
	profile := DefaultProfile()
	profile.SeriesBudget = minSeriesBudget
	for index := range profile.Dimensions {
		if profile.Dimensions[index].Key == RequestModel || profile.Dimensions[index].Key == ToolMetricLabelTool {
			for value := range maxProfileEntries {
				profile.Dimensions[index].Values = append(
					profile.Dimensions[index].Values,
					fmt.Sprintf("known-%d", value),
				)
			}
		}
	}
	tracker, provider, metricExporter, _ := newTestTrackerWithMetrics(t, WithProfile(profile))
	var workers sync.WaitGroup
	// Act.
	for worker := range 10 {
		workers.Go(func() {
			for index := range 1000 {
				name := fmt.Sprintf("known-%d", index+worker*1000)
				err := tracker.RecordInteraction(
					context.Background(),
					Meta{Operation: "chat", RequestModel: name},
					Payload{},
					Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)},
				)
				if err != nil {
					t.Errorf("unexpected observation validation: %v", err)
					return
				}
				closeTestTool(t, tracker.Runtime(), name)
			}
		})
	}
	workers.Wait()
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: data point count, not label string length, proves the budget.
	hist := metrytest.FindInt64Histogram(t, metrics, metrytest.MetricSelector{Name: TokenUsageMetricName})
	assert.LessOrEqual(t, len(hist.DataPoints), profile.SeriesBudget)
	assert.InDelta(
		t,
		float64(10000),
		metrytest.Int64HistogramSumByAttr(
			t,
			metrics,
			metrytest.MetricSelector{Name: TokenUsageMetricName},
			TokenType,
			TokenTypeInput,
		),
		1e-9,
	)
	tool := metrytest.FindFloat64Histogram(t, metrics, metrytest.MetricSelector{Name: ToolDurationMetricName})
	assert.LessOrEqual(t, len(tool.DataPoints), profile.SeriesBudget)
	hasOverflow := false
	for _, point := range hist.DataPoints {
		assert.Equal(t, "measured", testutil.SpanStringAttr(t, point.Attributes, usageSourceKey))
		if value, found := point.Attributes.Value(overflowKey); found && value.AsBool() {
			hasOverflow = true
		}
	}
	assert.True(t, hasOverflow)
}

func TestDefaultProfileUnknownDimensionsRemainBounded(t *testing.T) {
	// Arrange.
	tracker, _, _, _ := newTestTrackerWithMetrics(t, WithProfile(DefaultProfile()))
	// Act.
	for index := range 10000 {
		attrs := tracker.boundedMetricAttributes(
			OperationDurationMetricName,
			[]attribute.KeyValue{
				attribute.String(RequestModel, strconv.Itoa(index)),
				attribute.String(ServerAddress, "https://secret/"+strings.Repeat("x", index%100)),
			},
		)
		// Assert.
		for _, attr := range attrs {
			assert.Equal(t, "unknown", attr.Value.AsString())
		}
	}
	assert.Len(t, tracker.dimensions.combinations[OperationDurationMetricName], 1)
}

func TestConformanceReadsExportedOperation(t *testing.T) {
	// Arrange: the fixture derives its evidence from the exporter.
	profile := DefaultProfile()
	tracker, provider, exporter := newTestTracker(t, WithProfile(profile))
	declaration := AdapterDeclaration{
		ProfileID:  profile.ID,
		Mapping:    profile.Mapping,
		Boundaries: []string{profileModelBoundary},
	}
	// Act.
	report, err := CheckConformance(testContract(profile, false),
		[]AdapterDeclaration{declaration},
		func() ([]ObservedBoundary, error) {
			recordErr := tracker.RecordInteraction(
				context.Background(), Meta{Operation: profileChatOperation}, Payload{}, Usage{},
			)
			if recordErr != nil {
				return nil, recordErr
			}
			flushTestProvider(t, provider)
			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			operation := testutil.SpanStubStringAttr(t, spans[0], OperationName)
			return []ObservedBoundary{
				{
					Boundary:   profileModelBoundary,
					Operation:  operation,
					Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: len(spans)},
					Fields: []ProfileField{
						{
							Name:      OperationName,
							Required:  true,
							Stability: StabilityDevelopment,
							Encoding:  EncodingString,
						},
					},
				},
			}, nil
		},
	)
	// Assert.
	require.NoError(t, err)
	assert.True(t, report.Passed())
}

func TestCostOverflowPreservesCurrency(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	profile.SeriesBudget = minSeriesBudget
	for index := range profile.Dimensions {
		if profile.Dimensions[index].Key == RequestModel {
			for value := range 30 {
				profile.Dimensions[index].Values = append(profile.Dimensions[index].Values, strconv.Itoa(value))
			}
		}
	}
	tracker, provider, metricExporter, _ := newTestTrackerWithMetrics(t, WithProfile(profile))
	// Act.
	for index := range 30 {
		for _, currency := range []string{profileCurrencyUSD, "EUR"} {
			require.NoError(
				t,
				tracker.RecordInteraction(
					context.Background(),
					Meta{Operation: profileChatOperation, RequestModel: strconv.Itoa(index)},
					Payload{},
					Usage{Cost: Observe(1.0, UnitCurrency, SourceProvider), Currency: currency},
				),
			)
		}
	}
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: an overflow bucket must not add different currency units together.
	sum := metrytest.FindFloat64Sum(t, metrics, metrytest.MetricSelector{Name: CostMetricName})
	assert.LessOrEqual(t, len(sum.DataPoints), profile.SeriesBudget)
	totals := map[string]float64{}
	for _, point := range sum.DataPoints {
		totals[testutil.SpanStringAttr(t, point.Attributes, CostCurrency)] += point.Value
	}
	assert.InDelta(t, 30.0, totals[profileCurrencyUSD], 1e-9)
	assert.InDelta(t, 30.0, totals["EUR"], 1e-9)
	assert.Len(t, totals, 2)
}

func closeTestTool(t *testing.T, runtime Runtime, name string) {
	t.Helper()
	_, handle, err := runtime.StartTool(context.Background(), ToolCall{Name: name})
	if err != nil {
		t.Errorf("unexpected tool start validation: %v", err)
		return
	}
	if err := handle.Close(); err != nil {
		t.Errorf("unexpected tool close diagnostic: %v", err)
	}
}

// testContract permits intentionally invalid snapshots for rejection tests.
func testContract(profile Profile, raw bool) Contract {
	return Contract{
		profile:      cloneProfile(profile),
		contextBytes: defaultMaxContextLength,
		eventBytes:   defaultMaxEventLength,
		raw:          raw,
	}
}
