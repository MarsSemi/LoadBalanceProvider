package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LoadBalanceProvider/src/proxy"
)

// -------------------------------------------------------------------------------------
func TestDeferredResponseWriterCommitsOnFirstResponsesEvent(t *testing.T) {
	_recorder := httptest.NewRecorder()
	_writer := newDeferredResponseWriter(_recorder, true)
	_writer.Header().Set("Content-Type", "text/event-stream")
	_writer.WriteHeader(http.StatusOK)
	_, _ = _writer.Write([]byte("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n"))
	if !_writer.Committed() {
		t.Fatal("first normal Responses event should commit response")
	}
	if !strings.Contains(_recorder.Body.String(), "response.created") {
		t.Fatalf("committed response lost first event: %s", _recorder.Body.String())
	}
}

// -------------------------------------------------------------------------------------
func TestDeferredResponseWriterKeepsFailureDiscardableBeforeContent(t *testing.T) {
	_recorder := httptest.NewRecorder()
	_writer := newDeferredResponseWriter(_recorder, true)
	_writer.Header().Set("Content-Type", "text/event-stream")
	_writer.WriteHeader(http.StatusOK)
	_, _ = _writer.Write([]byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"Selected model is at capacity\"}}}\n\n"))
	if _writer.Committed() || _recorder.Body.Len() != 0 {
		t.Fatal("pre-token failure must remain discardable")
	}
	_err := &proxy.ProviderStreamError{Message: "Selected model is at capacity", RetryableCapacity: true, ResponseForwarded: true}
	if !providerFailureCanRetryBeforeFirstToken(_err, _writer) {
		t.Fatal("capacity failure before first token should be retryable")
	}
}

// -------------------------------------------------------------------------------------
func TestDeferredResponseWriterDoesNotRetryAfterContent(t *testing.T) {
	_recorder := httptest.NewRecorder()
	_writer := newDeferredResponseWriter(_recorder, true)
	_, _ = _writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n"))
	if !_writer.Committed() {
		t.Fatal("reasoning token should commit response")
	}
	if providerFailureCanRetryBeforeFirstToken(errors.New("connection reset"), _writer) {
		t.Fatal("committed response must never retry")
	}
}

// -------------------------------------------------------------------------------------
func TestDeferredResponseWriterDoesNotTreatHeadersAsForwardedContent(t *testing.T) {
	_recorder := httptest.NewRecorder()
	_writer := newDeferredResponseWriter(_recorder, true)
	_writer.Header().Set("Content-Type", "text/event-stream")
	_writer.WriteHeader(http.StatusOK)
	if _writer.HasBufferedResponse() {
		t.Fatal("headers without an SSE event must remain replaceable")
	}
	if !providerFailureCanRetryBeforeFirstToken(errors.New("unexpected EOF"), _writer) {
		t.Fatal("connection failure after headers but before content should retry")
	}
}

// -------------------------------------------------------------------------------------
func TestDeferredResponseWriterHeartbeatCommitsAndFlushes(t *testing.T) {
	_recorder := httptest.NewRecorder()
	_writer := newDeferredResponseWriter(_recorder, true)
	_writer.Header().Set("Content-Type", "text/event-stream")
	_writer.WriteHeader(http.StatusOK)

	if _err := _writer.WriteStreamHeartbeat([]byte(": keep-alive\n\n")); _err != nil {
		t.Fatalf("write heartbeat: %v", _err)
	}
	if !_writer.Committed() {
		t.Fatal("heartbeat must commit the downstream SSE response")
	}
	if _recorder.Body.String() != ": keep-alive\n\n" {
		t.Fatalf("heartbeat body = %q", _recorder.Body.String())
	}
	if !_recorder.Flushed {
		t.Fatal("heartbeat must flush the downstream response")
	}
}
