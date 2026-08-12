package balancer

import "math"

const (
	// token 速率的平滑係數。0.25 對逐筆請求的差異過於靈敏，儀表板會明顯跳動；
	// 0.1 需要約 10 筆請求才完成一次收斂，趨勢仍看得出來但不會忽高忽低。
	tokenRateEWMAAlpha          = 0.1
	suspiciousTokenRateFloor    = 1000.0
	maximumBurstToEndToEndRatio = 20.0
)

// NormalizeTokenRate rejects transport-burst samples that cannot represent
// sustained model generation. Legitimate high rates are retained when the
// request's end-to-end throughput supports them.
func NormalizeTokenRate(_tokens int64, _durationMS float64, _rate float64) float64 {
	if _rate <= 0 || math.IsNaN(_rate) || math.IsInf(_rate, 0) {
		return 0
	}
	if _tokens <= 0 || _durationMS <= 0 {
		return _rate
	}
	_endToEndRate := float64(_tokens) / (_durationMS / 1000)
	if _endToEndRate <= 0 {
		return _rate
	}
	if _rate > suspiciousTokenRateFloor && _rate > _endToEndRate*maximumBurstToEndToEndRatio {
		return _endToEndRate
	}
	return _rate
}
