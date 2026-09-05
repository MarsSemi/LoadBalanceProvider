package api

import (
	"context"
	"errors"
	"time"

	"LoadBalanceProvider/src/balancer"
)

const providerCooldownWaitBudget = 30 * time.Second

// 等待消耗獨立的累計預算，不因換 Provider 重設，也不計作一次實際上游請求。
func waitForProviderCooldown(ctx context.Context, writer *deferredResponseWriter, heartbeat []byte, selectionErr error, budget time.Duration) (time.Duration, bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	var unavailable *balancer.NoAvailableProviderError
	if !errors.As(selectionErr, &unavailable) || !unavailable.TemporaryOverload || unavailable.RetryAfter <= 0 || budget <= 0 {
		return 0, false
	}
	wait := unavailable.RetryAfter
	if wait > budget {
		return 0, false
	}
	started := time.Now()
	if err := writer.WriteStreamHeartbeat(heartbeat); err != nil {
		return 0, false
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	ticker := time.NewTicker(providerRetryKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return time.Since(started), false
		case <-timer.C:
			return time.Since(started), true
		case <-ticker.C:
			if err := writer.WriteStreamHeartbeat(heartbeat); err != nil {
				return time.Since(started), false
			}
		}
	}
}
