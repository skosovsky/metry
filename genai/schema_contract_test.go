package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry"
)

func TestSchemaDescriptorIsDetachedFromRuntimeConfiguration(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	for i := range profile.Dimensions {
		if profile.Dimensions[i].Key == ErrorType {
			profile.Dimensions[i].Values = []string{"local_error"}
		}
	}
	contract, err := NewContract(WithProfile(profile), WithPayloadByteLimit(128), WithEventTextByteLimit(17))
	require.NoError(t, err)
	schema := contract.Schema()
	fields, boundaries := schema.Fields(), schema.Boundaries()
	originalField, originalBoundary := fields[0], boundaries[0]
	// Act.
	fields[0].Name = "changed"
	boundaries[0].Accounting = false
	enums := contract.Enumerations()
	values := schemaEnumValues(t, enums, ErrorType)
	require.Equal(t, []string{"local_error"}, values)
	values[0] = "changed"
	// Assert: neither detached views nor runtime settings configure the schema.
	require.NoError(t, contract.Validate())
	require.Equal(t, InstrumentationProfileID, schema.ID())
	require.Equal(t, profileInclusiveMapping, schema.Mapping())
	require.Equal(t, originalField, Schema().Fields()[0])
	require.Equal(t, originalBoundary, Schema().Boundaries()[0])
	require.Equal(t, Schema().Fields(), schema.Fields())
	require.Equal(t, Schema().Boundaries(), schema.Boundaries())
	require.NotContains(t, schemaEnumValues(t, Schema().Enumerations(), ErrorType), "local_error")
	require.Equal(t, []string{"local_error"}, schemaEnumValues(t, contract.Enumerations(), ErrorType))
	require.Equal(t, 128, contract.PayloadByteLimit())
	require.Equal(t, 17, contract.EventTextByteLimit())
	for _, channel := range contract.PayloadChannels() {
		if channel.Attribute == EvaluationExplanation {
			require.Equal(t, "event_text_byte_limit", channel.BudgetSetting)
			require.Equal(t, contract.EventTextByteLimit(), channel.OutputByteLimit)
		} else {
			require.Equal(t, "payload_byte_limit", channel.BudgetSetting)
			require.Equal(t, contract.PayloadByteLimit(), channel.OutputByteLimit)
		}
	}
}

type scopeMeterProvider struct {
	metric.MeterProvider

	name   string
	config metric.MeterConfig
}

func (p *scopeMeterProvider) Meter(name string, options ...metric.MeterOption) metric.Meter {
	p.name, p.config = name, metric.NewMeterConfig(options...)
	return p.MeterProvider.Meter(name, options...)
}

type scopeTracerProvider struct {
	trace.TracerProvider

	name   string
	config trace.TracerConfig
}

func (p *scopeTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	p.name, p.config = name, trace.NewTracerConfig(options...)
	return p.TracerProvider.Tracer(name, options...)
}

func TestTrackerTypedSDKUsesExactInstrumentationScopes(t *testing.T) {
	// Arrange.
	meter := &scopeMeterProvider{MeterProvider: metricnoop.NewMeterProvider()}
	tracer := &scopeTracerProvider{TracerProvider: tracenoop.NewTracerProvider()}
	provider, err := metry.FromSDK(metry.SDKDependencies{
		MeterProvider: meter, TracerProvider: tracer, Propagator: propagation.TraceContext{},
	})
	require.NoError(t, err)
	// Act.
	tracker, err := NewTrackerFromProvider(provider)
	// Assert.
	require.NoError(t, err)
	require.NotNil(t, tracker)
	require.Equal(t, "metry/genai", meter.name)
	require.Equal(t, meter.name, tracer.name)
	require.Equal(t, "semconv-genai-e07f4eb+metry-v1", meter.config.InstrumentationVersion())
	require.Equal(t, meter.config.InstrumentationVersion(), tracer.config.InstrumentationVersion())
	require.Equal(t, "https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev", meter.config.SchemaURL())
	require.Equal(t, meter.config.SchemaURL(), tracer.config.SchemaURL())
}

func TestTrackerZeroProviderUsesTypedSDKNoops(t *testing.T) {
	// Arrange.
	provider := &metry.Provider{}
	// Act.
	tracker, err := NewTrackerFromProvider(provider)
	// Assert: the zero provider follows SDK's explicit noop dependency contract.
	require.NoError(t, err)
	require.NoError(t, tracker.Recorder().RecordOperation(context.Background(),
		Operation{Name: "chat"}, OperationResult{}))
}

func schemaEnumValues(t *testing.T, enums []EnumDeclaration, name string) []string {
	t.Helper()
	for _, enum := range enums {
		if enum.Name == name {
			return enum.Values
		}
	}
	t.Fatalf("missing enum %s", name)
	return nil
}
