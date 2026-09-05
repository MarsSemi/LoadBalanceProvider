package balancer

import (
	"strings"
	"sync/atomic"
	"time"
)

func (p *ProviderRuntime) ModelUnavailableUntil(model string) time.Time {
	p.modelCooldownLock.Lock()
	defer p.modelCooldownLock.Unlock()
	return p.modelCooldowns[strings.ToLower(strings.TrimSpace(model))]
}

func (p *ProviderRuntime) MarkModelUnavailable(model string, latency, duration time.Duration) {
	if duration <= 0 {
		duration = 30 * time.Second
	}
	now := time.Now()
	p.modelCooldownLock.Lock()
	if p.modelCooldowns == nil {
		p.modelCooldowns = make(map[string]time.Time)
	}
	// 只保留仍有效的項目，避免動態模型名稱讓快取無限成長。
	for key, until := range p.modelCooldowns {
		if !until.After(now) {
			delete(p.modelCooldowns, key)
		}
	}
	key := strings.ToLower(strings.TrimSpace(model))
	if !p.modelCooldowns[key].After(now) {
		if len(p.modelCooldowns) < 256 {
			p.modelCooldowns[key] = now.Add(duration)
		}
	}
	p.modelCooldownLock.Unlock()
	atomic.AddInt64(&p.Failures, 1)
	p.recordLatency(latency)
}
