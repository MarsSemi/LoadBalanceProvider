package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeartbeatHeadersAndFailureRecoverySmoke(t *testing.T) {
	r := httptest.NewRecorder()
	w := newDeferredResponseWriter(r, true)
	if err := w.WriteStreamHeartbeat([]byte("event: ping\ndata: {}\n\n")); err != nil {
		t.Fatal(err)
	}
	response := r.Result()
	if response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("incorrect initial SSE headers: %v", response.Header)
	}
	w.WriteHeader(http.StatusBadGateway)
	w.Write([]byte(`{"error":"upstream failure"}`))
	if w.ContentWritten() || strings.Contains(r.Body.String(), "upstream failure") {
		t.Fatal("failure leaked after heartbeat")
	}
	if !w.ResetForGracefulTerminal() {
		t.Fatal("heartbeat prevented recovery")
	}
	w.Write([]byte("event: response.failed\ndata: {}\n\n"))
	w.Commit()
	if strings.Contains(r.Body.String(), "upstream failure") || !strings.Contains(r.Body.String(), "response.failed") {
		t.Fatalf("invalid recovered SSE: %s", r.Body.String())
	}
}

func TestHeartbeatConcurrentHeadersSmoke(t *testing.T) {
	w := newDeferredResponseWriter(httptest.NewRecorder(), true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			w.Header().Set("X-Proxy-Model", "model")
		}
	}()
	for i := 0; i < 100; i++ {
		w.WriteStreamHeartbeat([]byte(": ping\n\n"))
	}
	<-done
}
