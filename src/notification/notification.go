package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"LoadBalanceProvider/src/balancer"
	"LoadBalanceProvider/src/config"
	"LoadBalanceProvider/src/domain"
	"LoadBalanceProvider/src/security"
)

const (
	defaultConfigPath                     = "data/notification_target.json"
	providerMonitorPeriod                 = 30 * time.Second
	notificationRetryDelay                = 15 * time.Minute
	lowUsageThreshold                     = 5.0
	unavailableFailureNotificationMinimum = 6
)

var issueState = struct {
	sync.Mutex
	items map[string]issueRecord
}{
	items: map[string]issueRecord{},
}

type issueRecord struct {
	Active        bool
	LastAttemptAt time.Time
}

// -------------------------------------------------------------------------------------
func SendMessage(_target domain.NotificationTargetConfig, _message string) error {
	_targetURL := strings.TrimSpace(_target.URL)
	if _targetURL == "" {
		return fmt.Errorf("notification URL is required")
	}
	if _err := security.ValidateOutboundURL(_targetURL); _err != nil {
		return _err
	}

	_payload := strings.ReplaceAll(_target.Payload, "<msg>", escapedPayloadMessage(_message))
	_request, _err := http.NewRequest(http.MethodPost, _targetURL, strings.NewReader(_payload))
	if _err != nil {
		return _err
	}
	_request.Header.Set("Content-Type", "application/json")
	_request.Header.Set("Accept", "application/json")
	if _apiKey := strings.TrimSpace(_target.APIKey); _apiKey != "" {
		_request.Header.Set("Authorization", "Bearer "+_apiKey)
		_request.Header.Set("X-API-Key", _apiKey)
	}

	_client := security.GuardedHTTPClient(&http.Client{Timeout: 15 * time.Second})
	_response, _err := _client.Do(_request)
	if _err != nil {
		return _err
	}
	defer _response.Body.Close()

	if _response.StatusCode >= 200 && _response.StatusCode < 300 {
		return nil
	}

	_body, _ := io.ReadAll(io.LimitReader(_response.Body, 2048))
	_bodyText := strings.TrimSpace(string(_body))
	if _bodyText == "" {
		return fmt.Errorf("notification target returned status %d", _response.StatusCode)
	}
	return fmt.Errorf("notification target returned status %d: %s", _response.StatusCode, _bodyText)
}

// -------------------------------------------------------------------------------------
func SendConfiguredMessage(_configPath string, _message string) error {
	_target, _err := config.LoadNotificationTargetConfig(configPath(_configPath))
	if _err != nil {
		return _err
	}
	if strings.TrimSpace(_target.URL) == "" {
		return nil
	}
	return SendMessage(_target, _message)
}

// -------------------------------------------------------------------------------------
func StartProviderMonitor(_ctx context.Context, _balancer *balancer.LoadBalancer, _configPath string) {
	if _ctx == nil {
		_ctx = context.Background()
	}
	if _balancer == nil {
		return
	}

	go func() {
		evaluateProviders(_balancer, _configPath)
		_ticker := time.NewTicker(providerMonitorPeriod)
		defer _ticker.Stop()

		for {
			select {
			case <-_ctx.Done():
				return
			case <-_ticker.C:
				evaluateProviders(_balancer, _configPath)
			}
		}
	}()
}

// -------------------------------------------------------------------------------------
func evaluateProviders(_balancer *balancer.LoadBalancer, _configPath string) {
	_now := time.Now()
	for _, _provider := range _balancer.ProvidersSnapshot() {
		if !providerShouldNotify(_provider) {
			continue
		}
		evaluateProviderAuthIssue(_configPath, _provider)
		evaluateProviderUsageIssue(_configPath, _provider)
		evaluateProviderUnavailableIssue(_configPath, _provider, _now)
	}
}

// -------------------------------------------------------------------------------------
func providerShouldNotify(_provider *balancer.ProviderRuntime) bool {
	if _provider == nil || _provider.Config == nil {
		return false
	}
	if !_provider.Config.Enabled {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(_provider.Config.Role), "classifier")
}

// -------------------------------------------------------------------------------------
func evaluateProviderAuthIssue(_configPath string, _provider *balancer.ProviderRuntime) {
	_auth := _provider.AuthErrorSnapshot()
	_key := providerIssueKey(_provider, "auth_error")
	if !_auth.Active {
		resolveIssue(_key)
		return
	}

	_message := fmt.Sprintf("LLM Provider「%s」登入 TOKEN 已失效。錯誤：%s", providerDisplayName(_provider), defaultText(_auth.Message, "需要重新登入或更新金鑰"))
	notifyIssueOnce(_configPath, _key, _message)
}

// -------------------------------------------------------------------------------------
func evaluateProviderUsageIssue(_configPath string, _provider *balancer.ProviderRuntime) {
	_usage := _provider.UsageSnapshot()
	_key := providerIssueKey(_provider, "quota_low")
	_detail := lowUsageDetail(_usage)
	if _detail == "" {
		resolveIssue(_key)
		return
	}

	_message := fmt.Sprintf("LLM Provider「%s」剩餘額度低於 %.0f%%：%s", providerDisplayName(_provider), lowUsageThreshold, _detail)
	notifyIssueOnce(_configPath, _key, _message)
}

// -------------------------------------------------------------------------------------
func evaluateProviderUnavailableIssue(_configPath string, _provider *balancer.ProviderRuntime, _now time.Time) {
	_key := providerIssueKey(_provider, "unavailable")
	if _provider.HasAuthError() {
		resolveIssue(_key)
		return
	}

	_consecutiveFailures := atomic.LoadInt64(&_provider.ConsecutiveFailures)
	_circuitOpen := _provider.CircuitOpen(_now)
	if !_circuitOpen || _consecutiveFailures < unavailableFailureNotificationMinimum {
		resolveIssue(_key)
		return
	}

	_message := fmt.Sprintf("LLM Provider「%s」目前不可用。連續失敗：%d，熔斷狀態：%t", providerDisplayName(_provider), _consecutiveFailures, _circuitOpen)
	notifyIssue(_configPath, _key, _message)
}

// -------------------------------------------------------------------------------------
func notifyIssue(_configPath string, _key string, _message string) {
	notifyIssueWithRetryDelay(_configPath, _key, _message, notificationRetryDelay)
}

// -------------------------------------------------------------------------------------
func notifyIssueOnce(_configPath string, _key string, _message string) {
	if !shouldAttemptNewIssue(_key, time.Now()) {
		return
	}

	if _err := SendConfiguredMessage(_configPath, _message); _err != nil {
		log.Printf("notification send failed: issue=%s error=%v", _key, _err)
		return
	}
}

// -------------------------------------------------------------------------------------
func notifyIssueWithRetryDelay(_configPath string, _key string, _message string, _retryDelay time.Duration) {
	_now := time.Now()
	if !shouldAttemptIssue(_key, _now, _retryDelay) {
		return
	}

	if _err := SendConfiguredMessage(_configPath, _message); _err != nil {
		log.Printf("notification send failed: issue=%s error=%v", _key, _err)
		return
	}
}

// -------------------------------------------------------------------------------------
func shouldAttemptNewIssue(_key string, _now time.Time) bool {
	issueState.Lock()
	defer issueState.Unlock()

	_record := issueState.items[_key]
	if _record.Active {
		return false
	}
	issueState.items[_key] = issueRecord{Active: true, LastAttemptAt: _now}
	return true
}

// -------------------------------------------------------------------------------------
func shouldAttemptIssue(_key string, _now time.Time, _retryDelay time.Duration) bool {
	issueState.Lock()
	defer issueState.Unlock()

	if _retryDelay <= 0 {
		_retryDelay = notificationRetryDelay
	}
	_record := issueState.items[_key]
	if _record.Active && _now.Sub(_record.LastAttemptAt) < _retryDelay {
		return false
	}
	issueState.items[_key] = issueRecord{Active: true, LastAttemptAt: _now}
	return true
}

// -------------------------------------------------------------------------------------
func resolveIssue(_key string) {
	issueState.Lock()
	delete(issueState.items, _key)
	issueState.Unlock()
}

// -------------------------------------------------------------------------------------
func lowUsageDetail(_usage balancer.ProviderUsageSnapshot) string {
	if !_usage.HasUsageInfo() {
		return ""
	}

	_parts := []string{}
	if codexRemainingKnownAndLow(_usage, "x-codex-primary-used-percent", _usage.CodexPrimaryRemainPercent) {
		_parts = append(_parts, fmt.Sprintf("五小時剩餘 %.1f%%", _usage.CodexPrimaryRemainPercent))
	}
	if codexRemainingKnownAndLow(_usage, "x-codex-secondary-used-percent", _usage.CodexSecondaryRemainPercent) {
		_parts = append(_parts, fmt.Sprintf("7日剩餘 %.1f%%", _usage.CodexSecondaryRemainPercent))
	}
	if _limit, _remaining := numberFromText(_usage.LimitRequests), numberFromText(_usage.RemainingRequests); _limit > 0 && _remaining >= 0 && _usage.RequestRemainingPercent <= lowUsageThreshold {
		_parts = append(_parts, fmt.Sprintf("請求剩餘 %.1f%%（%s/%s）", _usage.RequestRemainingPercent, _usage.RemainingRequests, _usage.LimitRequests))
	}
	if _limit, _remaining := numberFromText(_usage.LimitTokens), numberFromText(_usage.RemainingTokens); _limit > 0 && _remaining >= 0 && _usage.TokenRemainingPercent <= lowUsageThreshold {
		_parts = append(_parts, fmt.Sprintf("Token 剩餘 %.1f%%（%s/%s）", _usage.TokenRemainingPercent, _usage.RemainingTokens, _usage.LimitTokens))
	}
	return strings.Join(_parts, "；")
}

// -------------------------------------------------------------------------------------
func codexRemainingKnownAndLow(_usage balancer.ProviderUsageSnapshot, _usedHeaderKey string, _remainingPercent float64) bool {
	if len(_usage.Headers) == 0 {
		return false
	}
	_usedPercent := numberFromText(_usage.Headers[strings.ToLower(strings.TrimSpace(_usedHeaderKey))])
	if _usedPercent < 0 {
		return false
	}
	return _remainingPercent <= lowUsageThreshold
}

// -------------------------------------------------------------------------------------
func providerIssueKey(_provider *balancer.ProviderRuntime, _kind string) string {
	if _provider == nil || _provider.Config == nil {
		return _kind
	}
	return strings.TrimSpace(_provider.Config.ID) + ":" + _kind
}

// -------------------------------------------------------------------------------------
func providerDisplayName(_provider *balancer.ProviderRuntime) string {
	if _provider == nil || _provider.Config == nil {
		return "未知來源"
	}
	if _name := strings.TrimSpace(_provider.Config.Name); _name != "" {
		return _name
	}
	if _id := strings.TrimSpace(_provider.Config.ID); _id != "" {
		return _id
	}
	return "未命名來源"
}

// -------------------------------------------------------------------------------------
func escapedPayloadMessage(_message string) string {
	_bytes, _err := json.Marshal(_message)
	if _err != nil || len(_bytes) < 2 {
		return _message
	}
	return string(_bytes[1 : len(_bytes)-1])
}

// -------------------------------------------------------------------------------------
func numberFromText(_text string) float64 {
	_text = strings.TrimSpace(strings.ToLower(_text))
	if _text == "" {
		return -1
	}
	_text = strings.TrimSuffix(_text, "%")
	_text = strings.ReplaceAll(_text, ",", "")
	var _value float64
	if _, _err := fmt.Sscanf(_text, "%f", &_value); _err != nil {
		return -1
	}
	return _value
}

// -------------------------------------------------------------------------------------
func configPath(_path string) string {
	if strings.TrimSpace(_path) != "" {
		return _path
	}
	return defaultConfigPath
}

// -------------------------------------------------------------------------------------
func defaultText(_value string, _fallback string) string {
	if strings.TrimSpace(_value) != "" {
		return strings.TrimSpace(_value)
	}
	return _fallback
}
