package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type FailureDetails struct {
	Code       string
	RetryAfter time.Duration
}

// HTTP 錯誤的本文保留在 deferred writer，讀完後再補入結構化分類資訊。
func EnrichFailure(err error, body string) {
	var status *ProviderStatusError
	if !errors.As(err, &status) {
		return
	}
	var payload map[string]interface{}
	if json.Unmarshal([]byte(body), &payload) != nil {
		return
	}
	if nested, ok := payload["error"].(map[string]interface{}); ok {
		payload = nested
	}
	status.Code = strings.ToLower(stringFromAny(payload["code"]))
	if status.Code == "" {
		status.Code = strings.ToLower(stringFromAny(payload["type"]))
	}
	status.Message = stringFromAny(payload["message"])
}

func failureDetailsFromEvent(event string) FailureDetails {
	var details FailureDetails
	for _, payload := range responseEventPayloads(event) {
		if response, ok := payload["response"].(map[string]interface{}); ok {
			payload = response
		}
		if nested, ok := payload["error"].(map[string]interface{}); ok {
			payload = nested
		}
		details.Code = strings.ToLower(stringFromAny(payload["code"]))
		if details.Code == "" {
			details.Code = strings.ToLower(stringFromAny(payload["type"]))
		}
		if seconds, ok := payload["retry_after"].(float64); ok && seconds > 0 && seconds <= 86400 {
			details.RetryAfter = time.Duration(seconds * float64(time.Second))
		}
	}
	return details
}

func retryAfterHeader(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 && seconds <= 86400 {
		return time.Duration(seconds * float64(time.Second))
	}
	if deadline, err := http.ParseTime(value); err == nil {
		if wait := time.Until(deadline); wait > 0 && wait <= 24*time.Hour {
			return wait
		}
	}
	return 0
}

// Request 表示請求本身有誤，不重試也不懲罰帳號；ModelOnly 避免誤停其他模型。
type FailurePolicy struct {
	Request    bool
	Capacity   bool
	ModelOnly  bool
	RetryAfter time.Duration
}

func ClassifyFailure(err error) FailurePolicy {
	if err == nil {
		return FailurePolicy{}
	}
	var stream *ProviderStreamError
	var status *ProviderStatusError
	var details FailureDetails
	code := 0
	if errors.As(err, &stream) {
		details = stream.FailureDetails
	}
	if errors.As(err, &status) {
		details = status.FailureDetails
		code = status.StatusCode
	}
	text := strings.ToLower(err.Error() + " " + details.Code)
	policy := FailurePolicy{RetryAfter: details.RetryAfter}
	for _, marker := range []string{"context_length_exceeded", "maximum context length", "invalid_request_error", "cyber_policy", "content_policy_violation"} {
		if strings.Contains(text, marker) {
			policy.Request = true
			return policy
		}
	}
	// 模型支援錯誤仍可換其他 Provider，但只隔離該模型。
	if strings.Contains(text, "model_not_found") || strings.Contains(text, "model not found") {
		policy.Capacity, policy.ModelOnly = true, true
		return policy
	}
	if code == 400 || code == 422 {
		policy.Request = true
		return policy
	}
	for _, marker := range []string{"usage_limit_reached", "insufficient_quota", "quota_exceeded", "rate_limit_exceeded"} {
		if strings.Contains(text, marker) {
			policy.Capacity = true
			return policy
		}
	}
	policy.Capacity = IsRetryableCapacityError(err) || code == 429 || code == 503
	policy.ModelOnly = policy.Capacity && (strings.Contains(text, "overloaded") || strings.Contains(text, "at capacity") || strings.Contains(text, "model_capacity"))
	return policy
}
