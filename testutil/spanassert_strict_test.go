package testutil_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry/testutil"
)

func TestAttributeAssertionsRejectWrongTypes(t *testing.T) {
	if kind := os.Getenv("METRY_ATTR_FAILURE_CASE"); kind != "" {
		attrs := attribute.NewSet(attribute.String("value", ""))
		switch kind {
		case "string":
			testutil.SpanStringAttr(t, attribute.NewSet(attribute.Int("value", 0)), "value")
		case "int":
			testutil.SpanInt64Attr(t, attrs, "value")
		case "float":
			testutil.SpanFloat64Attr(t, attrs, "value")
		case "bool":
			testutil.SpanBoolAttr(t, attrs, "value")
		case "slice":
			testutil.SpanStubFloat64SliceAttr(t, tracetest.SpanStub{Attributes: attrs.ToSlice()}, "value")
		}
		return
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, kind := range []string{"string", "int", "float", "bool", "slice"} {
		t.Run(kind, func(t *testing.T) {
			// Arrange: invoke the real fatal assertion in a separate test process.
			cmd := exec.Command(binary, "-test.run=^TestAttributeAssertionsRejectWrongTypes$")
			cmd.Env = append(os.Environ(), "METRY_ATTR_FAILURE_CASE="+kind)
			// Act.
			output, err := cmd.CombinedOutput()
			// Assert: neither a wrong type nor its implicit zero value is accepted.
			require.Error(t, err)
			require.Contains(t, string(output), "wrong type")
		})
	}
}

func TestAttributeAssertionsAcceptTypedZeroValues(t *testing.T) {
	// Arrange.
	attrs := attribute.NewSet(attribute.String("string", ""), attribute.Int64("int", 0),
		attribute.Float64("float", 0), attribute.Bool("bool", false), attribute.Float64Slice("slice", []float64{}))
	span := tracetest.SpanStub{Attributes: attrs.ToSlice()}
	// Act and Assert.
	require.Empty(t, testutil.SpanStringAttr(t, attrs, "string"))
	require.Zero(t, testutil.SpanInt64Attr(t, attrs, "int"))
	require.Zero(t, testutil.SpanFloat64Attr(t, attrs, "float"))
	require.False(t, testutil.SpanBoolAttr(t, attrs, "bool"))
	require.Empty(t, testutil.SpanStubFloat64SliceAttr(t, span, "slice"))
}
