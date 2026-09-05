package proxy

import (
	"encoding/json"
	"testing"

	"LoadBalanceProvider/src/domain"
)

func TestCodexAccountAPIURL(t *testing.T) {
	_tests := []struct {
		name     string
		baseURL  string
		resource string
		want     string
	}{
		{
			name:     "ChatGPT host uses WHAM paths",
			baseURL:  "https://chatgpt.com",
			resource: "rate-limit-reset-credits",
			want:     "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits",
		},
		{
			name:     "ChatGPT backend path is not duplicated",
			baseURL:  "https://chatgpt.com/backend-api/",
			resource: "/usage",
			want:     "https://chatgpt.com/backend-api/wham/usage",
		},
		{
			name:     "Codex API compatible host uses Codex API paths",
			baseURL:  "https://codex.example.com",
			resource: "rate-limit-reset-credits/consume",
			want:     "https://codex.example.com/api/codex/rate-limit-reset-credits/consume",
		},
	}

	for _, _test := range _tests {
		t.Run(_test.name, func(t *testing.T) {
			_provider := &domain.LLMProviderConfig{BaseURL: _test.baseURL}
			if _got := codexAccountAPIURL(_provider, _test.resource); _got != _test.want {
				t.Fatalf("codexAccountAPIURL() = %q, want %q", _got, _test.want)
			}
		})
	}
}

func TestCodexAccountAPIErrorMessage(t *testing.T) {
	if _got := codexAccountAPIErrorMessage([]byte(`{"error":{"message":"reset unavailable"}}`)); _got != "reset unavailable" {
		t.Fatalf("error message = %q", _got)
	}
}

func TestEarliestExpiringCodexResetCreditID(t *testing.T) {
	_expiringSoon := "2026-09-21T00:05:00Z"
	_expiringLater := "2026-10-04T02:06:00Z"
	_invalidExpiry := "invalid"
	_credits := []codexRateLimitResetCreditPayload{
		{ID: "redeemed", Status: "redeemed", ExpiresAt: &_expiringSoon},
		{ID: "no-expiry", Status: "available"},
		{ID: "later", Status: "available", ExpiresAt: &_expiringLater},
		{ID: "invalid-expiry", Status: "available", ExpiresAt: &_invalidExpiry},
		{ID: " soon ", Status: "AVAILABLE", ExpiresAt: &_expiringSoon},
	}

	if _got := earliestExpiringCodexResetCreditID(_credits); _got != "soon" {
		t.Fatalf("earliest credit ID = %q, want %q", _got, "soon")
	}
}

func TestEarliestExpiringCodexResetCreditIDKeepsUnknownExpiryLast(t *testing.T) {
	_expiry := "2026-10-04T02:06:00Z"
	_credits := []codexRateLimitResetCreditPayload{
		{ID: "no-expiry", Status: "available"},
		{ID: "dated", Status: "available", ExpiresAt: &_expiry},
	}

	if _got := earliestExpiringCodexResetCreditID(_credits); _got != "dated" {
		t.Fatalf("earliest credit ID = %q, want %q", _got, "dated")
	}
}

func TestCodexRateLimitResetConsumeRequestIncludesSelectedCredit(t *testing.T) {
	_body, _err := json.Marshal(codexRateLimitResetConsumeRequest{
		RedeemRequestID: "request-1",
		CreditID:        "credit-1",
	})
	if _err != nil {
		t.Fatal(_err)
	}
	if _got, _want := string(_body), `{"redeem_request_id":"request-1","credit_id":"credit-1"}`; _got != _want {
		t.Fatalf("request body = %s, want %s", _got, _want)
	}
}

func TestCodexRateLimitResetConsumeRequestOmitsCreditWithoutDetails(t *testing.T) {
	_body, _err := json.Marshal(codexRateLimitResetConsumeRequest{RedeemRequestID: "request-1"})
	if _err != nil {
		t.Fatal(_err)
	}
	if _got, _want := string(_body), `{"redeem_request_id":"request-1"}`; _got != _want {
		t.Fatalf("request body = %s, want %s", _got, _want)
	}
}
