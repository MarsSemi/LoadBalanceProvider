package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"LoadBalanceProvider/src/security"
)

type providerResponseBody struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
	timer  *time.Timer
}

func (b *providerResponseBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel(nil)
	return b.ReadCloser.Close()
}

// 僅限制等待回應標頭的階段；已開始的串流交由無進展逾時管理。
func doProviderHTTPRequest(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	expired := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		cancel(context.DeadlineExceeded)
		close(expired)
	})
	resp, err := security.GuardedHTTPClient(client).Do(req.Clone(ctx))
	if !timer.Stop() {
		<-expired
	}
	if cause := context.Cause(ctx); cause != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		cancel(nil)
		return nil, fmt.Errorf("等待上游回應標頭失敗: %w", cause)
	}
	if err != nil {
		cancel(nil)
		return nil, err
	}
	body := &providerResponseBody{ReadCloser: resp.Body, cancel: cancel}
	// 錯誤回應也可能只送標頭後停住，讀取錯誤本文不得無限等待。
	if resp.StatusCode >= http.StatusBadRequest {
		body.timer = time.AfterFunc(timeout, func() { cancel(context.DeadlineExceeded) })
	}
	resp.Body = body
	return resp, nil
}
