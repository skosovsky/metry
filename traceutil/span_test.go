package traceutil

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// mockSpan records RecordError and SetStatus calls for tests.
type mockSpan struct {
	noop.Span

	recordErrorCalled bool
	setStatusCalled   bool
	errRecorded       error
	statusCode        codes.Code
	statusDesc        string
}

func (m *mockSpan) RecordError(err error, _ ...trace.EventOption) {
	m.recordErrorCalled = true
	m.errRecorded = err
}

func (m *mockSpan) SetStatus(code codes.Code, description string) {
	m.setStatusCalled = true
	m.statusCode = code
	m.statusDesc = description
}

func TestSpanError_NilError_DoesNothing(t *testing.T) {
	m := &mockSpan{}
	SpanError(m, nil)
	require.False(t, m.recordErrorCalled, "RecordError must not be called when err is nil")
	require.False(t, m.setStatusCalled, "SetStatus must not be called when err is nil")
	require.NoError(t, m.errRecorded, "recorded error must remain nil")
	require.Equal(t, codes.Unset, m.statusCode, "status must remain Unset")
}

func TestSpanError_NonNilError_SetsErrorStatusAndRecordsError(t *testing.T) {
	m := &mockSpan{}
	err := errors.New("something failed")
	SpanError(m, err)
	require.Equal(t, err, m.errRecorded, "RecordError must receive the same error")
	require.Equal(t, codes.Error, m.statusCode, "span status must be Error")
	require.Equal(t, err.Error(), m.statusDesc, "status description must be error message")
}
