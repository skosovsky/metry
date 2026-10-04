package genai

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
)

func TestCorruptedContinuationTokensRejectBeforeAdapterRecording(t *testing.T) {
	// Arrange: token parsing is the host adapter's first boundary. A failed
	// parse never manufactures a reference or starts a replacement observation.
	tracker, provider, exporter := newTestTracker(t)
	for _, token := range []string{
		"private_token_marker!",
		base64.RawURLEncoding.EncodeToString([]byte(`{"trace_id":"private_token_marker","span_id":"broken"}`)),
	} {
		// Act.
		handle, err := metry.ParseAsyncHandle(token)
		// Assert.
		require.ErrorIs(t, err, metry.ErrInvalidAsyncHandle)
		assert.False(t, handle.IsValid())
		assert.NotContains(t, err.Error(), "private_token_marker")
	}
	for _, token := range []string{
		"private_token_marker!",
		base64.RawURLEncoding.EncodeToString([]byte(`{"traceparent":"private_token_marker"}`)),
	} {
		// Act.
		snapshot, err := metry.ParseTraceSnapshot(token)
		// Assert.
		require.ErrorIs(t, err, metry.ErrInvalidTraceSnapshot)
		assert.False(t, snapshot.IsValid())
		assert.NotContains(t, err.Error(), "private_token_marker")
	}
	// Act: bounds are checked before decode, and an invalid typed link cannot
	// bypass token validation by entering the lifecycle boundary directly.
	_, err := metry.ParseAsyncHandle(strings.Repeat("x", 4096))
	require.ErrorIs(t, err, metry.ErrHandleTokenTooLarge)
	_, err = metry.ParseTraceSnapshot(strings.Repeat("x", 4096))
	require.ErrorIs(t, err, metry.ErrTraceSnapshotTokenTooLarge)
	_, recording, err := tracker.Runtime().StartOperation(context.Background(),
		Operation{Name: profileChatOperation}, RecordingOptions{Links: []metry.AsyncHandle{{}}})
	flushTestProvider(t, provider)
	// Assert.
	require.ErrorIs(t, err, metry.ErrInvalidAsyncHandle)
	assert.Nil(t, recording)
	assert.Empty(t, exporter.GetSpans())
}
