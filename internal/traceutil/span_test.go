package traceutil

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

func (m *mockSpan) IsRecording() bool {
	return true
}

func (m *mockSpan) Status() sdktrace.Status {
	return sdktrace.Status{Code: m.statusCode, Description: m.statusDesc}
}

func TestMutateRecordingSpan_NotRecording_NoOp(t *testing.T) {
	called := false
	MutateRecordingSpan(noop.Span{}, func(trace.Span) {
		called = true
	})
	require.False(t, called, "fn must not run when span is not recording")
}

func TestMutateRecordingSpan_Recording_RunsFn(t *testing.T) {
	m := &mockSpan{}
	called := false
	MutateRecordingSpan(m, func(trace.Span) {
		called = true
	})
	require.True(t, called)
}

func TestSpanOK_SetsOkStatus(t *testing.T) {
	m := &mockSpan{}
	SpanOK(m)
	require.True(t, m.setStatusCalled, "SetStatus must be called")
	require.Equal(t, codes.Ok, m.statusCode, "span status must be Ok")
	require.Empty(t, m.statusDesc, "status description must be empty")
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

func TestSpanOKIfUnset_Unset_SetsOk(t *testing.T) {
	m := &mockSpan{}
	SpanOKIfUnset(m)
	require.Equal(t, codes.Ok, m.statusCode)
}

func TestSpanOKIfUnset_AlreadyError_NoOverwrite(t *testing.T) {
	m := &mockSpan{statusCode: codes.Error, statusDesc: "failed"}
	SpanOKIfUnset(m)
	require.Equal(t, codes.Error, m.statusCode)
	require.Equal(t, "failed", m.statusDesc)
}

func TestSpanOKIfUnset_AlreadyOk_NoOverwrite(t *testing.T) {
	m := &mockSpan{statusCode: codes.Ok}
	SpanOKIfUnset(m)
	require.Equal(t, codes.Ok, m.statusCode)
	require.False(t, m.setStatusCalled, "SetStatus must not be called when already Ok")
}
