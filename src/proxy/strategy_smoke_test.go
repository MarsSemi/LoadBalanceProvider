package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"LoadBalanceProvider/src/balancer"
	"LoadBalanceProvider/src/codexauth"
	"LoadBalanceProvider/src/domain"
)

func TestFailurePolicySmoke(t *testing.T) {
	for _, test := range []struct {
		code                     string
		status                   int
		request, capacity, model bool
	}{
		{"server_is_overloaded", 503, false, true, true},
		{"usage_limit_reached", 429, false, true, false},
		{"model_not_found", 404, false, true, true},
		{"context_length_exceeded", 400, true, false, false},
	} {
		err := &ProviderStatusError{StatusCode: test.status}
		EnrichFailure(err, `{"error":{"code":"`+test.code+`","message":"failure"}}`)
		policy := ClassifyFailure(err)
		if policy.Request != test.request || policy.Capacity != test.capacity || policy.ModelOnly != test.model {
			t.Fatalf("%s: %+v", test.code, policy)
		}
	}
	if retryAfterHeader(http.Header{"Retry-After": []string{"12"}}) != 12*time.Second {
		t.Fatal("Retry-After not parsed")
	}
}

func TestCompletionRepairSmoke(t *testing.T) {
	r := completionRepair{}
	r.process("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"c1\",\"arguments\":\"{}\"}}\n\n")
	for _, output := range []string{`[]`, `[{"type":"function_call","call_id":"c1","arguments":"keep"}]`} {
		event := "data: {\"type\":\"response.completed\",\"sequence_number\":9,\"response\":{\"id\":\"resp_1\",\"output\":" + output + "}}\n\n"
		repaired := r.process(event)
		payload := responseEventPayloads(repaired)[0]
		response := payload["response"].(map[string]interface{})
		item := response["output"].([]interface{})[0].(map[string]interface{})
		if item["id"] != "fc_1" || payload["sequence_number"] != float64(9) {
			t.Fatalf("invalid repair: %s", repaired)
		}
		if strings.Contains(output, "keep") && item["arguments"] != "keep" {
			t.Fatal("overwrote existing arguments")
		}
	}
	event := `data: {"type":"response.completed","response":{"output":[{"id":"original","type":"function_call","call_id":"c1"}]}}` + "\n\n"
	if r.process(event) != event {
		t.Fatal("modified complete event")
	}
	for _, item := range []string{`{"type":[],"call_id":"c1"}`, `{"type":"function_call","call_id":{}}`} {
		event := `data: {"type":"response.completed","response":{"output":[` + item + `]}}` + "\n\n"
		if r.process(event) != event {
			t.Fatal("modified malformed item")
		}
	}
	data, _ := json.Marshal(r.items)
	if len(data) == 0 {
		t.Fatal("missing item cache")
	}
}

func TestCodexUnauthorizedRetrySmoke(t *testing.T) {
	for _, scenario := range []string{"recovered", "still_unauthorized", "refresh_failed", "api_key"} {
		t.Run(scenario, func(t *testing.T) {
			calls, refreshes := 0, 0
			client := &http.Client{Transport: timeoutSmokeTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(req.Body)
				if err != nil || string(body) != `{"input":"hello"}` {
					t.Fatalf("request not replayed intact: %s %v", body, err)
				}
				if calls > 1 && (req.Header.Get("Authorization") != "Bearer new" || req.Header.Get("chatgpt-account-id") != "account") {
					t.Fatal("retry did not use refreshed credentials")
				}
				status := http.StatusUnauthorized
				if calls == 2 && scenario == "recovered" {
					status = http.StatusOK
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			req, _ := http.NewRequest(http.MethodPost, "https://8.8.8.8/responses", strings.NewReader(`{"input":"hello"}`))
			req.Header.Set("Authorization", "Bearer old")
			provider := &balancer.ProviderRuntime{Config: &domain.LLMProviderConfig{ID: "provider", TimeoutSeconds: 1}}
			refresh := func(ctx context.Context, id, failed string) (codexauth.Auth, error) {
				refreshes++
				if ctx.Err() != nil || id != "provider" || failed != "old" {
					t.Fatal("invalid refresh context")
				}
				if scenario == "refresh_failed" {
					return codexauth.Auth{}, errors.New("refresh failed")
				}
				return codexauth.Auth{AccessToken: "new", AccountID: "account"}, nil
			}
			resp, err := doCodexHTTPRequestWithRefresh(client, req, provider, scenario == "api_key", refresh)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			wantCalls, wantRefreshes, wantStatus := 2, 1, http.StatusUnauthorized
			if scenario == "recovered" {
				wantStatus = http.StatusOK
			}
			if scenario == "api_key" || scenario == "refresh_failed" {
				wantCalls = 1
			}
			if scenario == "api_key" {
				wantRefreshes = 0
			}
			if calls != wantCalls || refreshes != wantRefreshes || resp.StatusCode != wantStatus {
				t.Fatalf("calls=%d refreshes=%d status=%d", calls, refreshes, resp.StatusCode)
			}
		})
	}
}
