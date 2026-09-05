package proxy

import (
	"LoadBalanceProvider/src/domain"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildCodexResponsesRequestMapsToolsAndToolChoice(t *testing.T) {
	request := &domain.ChatCompletionRequest{
		Model:      "gpt-5.6-luna",
		Messages:   []domain.ChatMessage{{Role: "user", Content: "列出目錄"}},
		ToolChoice: "required",
		Tools: []interface{}{map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "directory_list",
				"description": "列出目錄",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
				},
			},
		}},
	}

	payload := buildCodexResponsesRequest(request, request.Model, &domain.LLMProviderConfig{Kind: "openai-codex"})
	if payload.ToolChoice != "required" {
		t.Fatalf("tool choice = %#v, want required", payload.ToolChoice)
	}
	if len(payload.Tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", payload.Tools)
	}
	tool, ok := payload.Tools[0].(map[string]interface{})
	if !ok || tool["type"] != "function" || tool["name"] != "directory_list" {
		t.Fatalf("tool was not converted to Responses format: %#v", payload.Tools[0])
	}
	if _, nested := tool["function"]; nested {
		t.Fatalf("Responses tool must not retain Chat Completions function wrapper: %#v", tool)
	}
}

func TestBuildCodexResponsesRequestKeepsToolRoundHistory(t *testing.T) {
	request := &domain.ChatCompletionRequest{
		Model: "gpt-5.6-luna",
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "列出目錄"},
			{Role: "assistant", ToolCalls: []domain.ChatToolCall{{
				ID: "call_1", Type: "function", Function: domain.ChatFunctionCall{Name: "directory_list", Arguments: `{"path":"."}`},
			}}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"files":["README.md"]}`},
		},
	}

	payload := buildCodexResponsesRequest(request, request.Model, &domain.LLMProviderConfig{Kind: "openai-codex"})
	if len(payload.Input) != 3 {
		t.Fatalf("input = %#v, want user, function_call and function_call_output", payload.Input)
	}
	call := payload.Input[1]
	if call.Type != "function_call" || call.CallID != "call_1" || call.Name != "directory_list" || call.Args != `{"path":"."}` {
		t.Fatalf("function call history = %#v", call)
	}
	output := payload.Input[2]
	if output.Type != "function_call_output" || output.CallID != "call_1" || output.Output != `{"files":["README.md"]}` {
		t.Fatalf("function output history = %#v", output)
	}
}

func TestCodexCompletedAsChatReturnsToolCalls(t *testing.T) {
	result := codexCompletedAsChat(codexCompleted{
		ID:    "resp_1",
		Model: "gpt-5.6-luna",
		Output: []codexOutputItem{{
			ID: "fc_1", Type: "function_call", CallID: "call_1", Name: "directory_list", Arguments: `{"path":"."}`,
		}},
	}, "fallback")

	choices := result["choices"].([]map[string]interface{})
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("finish reason = %#v, want tool_calls", choices[0]["finish_reason"])
	}
	message := choices[0]["message"].(map[string]interface{})
	calls := message["tool_calls"].([]map[string]interface{})
	function := calls[0]["function"].(map[string]interface{})
	if calls[0]["id"] != "call_1" || function["name"] != "directory_list" || function["arguments"] != `{"path":"."}` {
		t.Fatalf("chat tool calls = %#v", calls)
	}
}

func TestCodexStreamConvertsFunctionCallEvents(t *testing.T) {
	stream := `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"directory_list","arguments":""}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"path\":\".\"}"}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"directory_list","arguments":"{\"path\":\".\"}"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-luna","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"directory_list","arguments":"{\"path\":\".\"}"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}

`
	recorder := httptest.NewRecorder()
	if _, err := streamCodexResponsesAsChat(recorder, strings.NewReader(stream), "gpt-5.6-luna", time.Now()); err != nil {
		t.Fatalf("stream conversion failed: %v", err)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`"id":"call_1"`, `"name":"directory_list"`, `"arguments":"{\"path\":\".\"}"`, `"finish_reason":"tool_calls"`, "data: [DONE]"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream output does not contain %q: %s", expected, body)
		}
	}
}
