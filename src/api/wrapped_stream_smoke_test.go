package api

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MarsSemi/MarsCloud-SaaS/SDK/HttpService"
)

// 模擬 SDK trackedResponseWriter：只嵌入介面，不提供 Flush 或 Unwrap。
type sdkTrackedSmokeWriter struct{ http.ResponseWriter }

type deadlineSmokeWriter struct {
	http.ResponseWriter
	deadlines []time.Time
}

func (w *deadlineSmokeWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

type flushErrorSmokeWriter struct {
	http.ResponseWriter
	err error
}

func (w *flushErrorSmokeWriter) FlushError() error { return w.err }

func TestWrappedHeartbeatFlushAndDeadlineSmoke(t *testing.T) {
	r := httptest.NewRecorder()
	d := &deadlineSmokeWriter{ResponseWriter: r}
	w := newDeferredResponseWriter(&sdkTrackedSmokeWriter{ResponseWriter: d}, true)
	if err := w.WriteStreamHeartbeat([]byte(": ping\n\n")); err != nil {
		t.Fatal(err)
	}
	if !r.Flushed || len(d.deadlines) != 2 || d.deadlines[0].IsZero() || !d.deadlines[1].IsZero() {
		t.Fatalf("flush/deadline not forwarded: flushed=%v deadlines=%v", r.Flushed, d.deadlines)
	}
}

func TestWrappedHeartbeatFlushErrorsSmoke(t *testing.T) {
	for _, unsupported := range []bool{true, false} {
		t.Run(fmt.Sprint(unsupported), func(t *testing.T) {
			sentinel := errors.New("downstream I/O failed")
			if unsupported {
				sentinel = fmt.Errorf("wrapped: %w", http.ErrNotSupported)
			}
			r := httptest.NewRecorder()
			w := newDeferredResponseWriter(&flushErrorSmokeWriter{ResponseWriter: r, err: sentinel}, true)
			err := w.WriteStreamHeartbeat([]byte(": ping\n\n"))
			if unsupported {
				if err != nil || !r.Flushed {
					t.Fatalf("fallback failed: %v", err)
				}
			} else if !errors.Is(err, sentinel) || r.Flushed {
				t.Fatalf("real I/O failure was ignored: %v", err)
			}
		})
	}
}

func TestWrappedHeartbeatLiveHTTPSmoke(t *testing.T) {
	release := make(chan struct{})
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compressed, closeCompression := HttpService.MaybeCompressWriter(w, r)
		defer closeCompression()
		deferred := newDeferredResponseWriter(&sdkTrackedSmokeWriter{ResponseWriter: compressed}, true)
		result <- deferred.WriteStreamHeartbeat([]byte(": ping\n\n"))
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	// 使用 Go HTTP client 的透明 gzip 解壓，驗證 SDK 壓縮層確實即時 Flush。
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != ": ping\n" {
		t.Fatalf("heartbeat not received before handler returns: %q %v", line, err)
	}
	if !resp.Uncompressed {
		t.Fatal("SDK compression path was not exercised")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
