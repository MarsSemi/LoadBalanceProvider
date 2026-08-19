package telemetry

import "testing"

func TestAnalyzeResponsesToolContinuation(t *testing.T) {
	_body := []byte(`{
		"previous_response_id":"resp_1",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Install curl"}]},
			{"type":"local_shell_call","call_id":"call_1"},
			{"type":"local_shell_call_output","call_id":"call_1","output":"installed successfully"}
		]
	}`)
	_signals := AnalyzeRequestJSON(_body)
	if !_signals.Continuation || _signals.ToolCalls != 1 || _signals.ToolRounds != 1 {
		t.Fatalf("signals = %#v", _signals)
	}
	if _signals.ToolOutputTokens <= 0 || _signals.Fingerprint == "" {
		t.Fatalf("responses tool telemetry missing: %#v", _signals)
	}
}

func TestAnalyzeChatCountsOnlyTrailingToolResults(t *testing.T) {
	_body := []byte(`{
		"messages":[
			{"role":"user","content":"List files"},
			{"role":"assistant","tool_calls":[{"id":"call_1","function":{"name":"exec_command"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"a.txt\nb.txt"}
		]
	}`)
	_signals := AnalyzeRequestJSON(_body)
	if _signals.ToolCalls != 1 || _signals.ToolOutputTokens <= 0 || !_signals.Continuation {
		t.Fatalf("signals = %#v", _signals)
	}
}

func TestPromptFingerprintNormalizesVolatileValues(t *testing.T) {
	_left := AnalyzeRequestJSON([]byte(`{"input":"  INSTALL   Package 12345678 "}`))
	_right := AnalyzeRequestJSON([]byte(`{"input":"install package 87654321"}`))
	if _left.Fingerprint == "" || _left.Fingerprint != _right.Fingerprint {
		t.Fatalf("fingerprints differ: %q != %q", _left.Fingerprint, _right.Fingerprint)
	}
}

// 續談那一輪的 input 常常只有 function_call_output，沒有工具名稱也要算得出工具數。
// 這正是移除工具分類後不再需要前一輪快照的原因。
func TestToolCallsCountedWithoutToolNames(t *testing.T) {
	_signals := AnalyzeRequestJSON([]byte(`{
		"previous_response_id":"resp_1",
		"input":[{"type":"function_call_output","call_id":"call_1","output":"done"}]
	}`))
	if _signals.ToolCalls != 1 || _signals.ToolRounds != 1 || !_signals.Continuation {
		t.Fatalf("tool signals should not depend on tool names: %#v", _signals)
	}
	if _signals.ToolOutputTokens <= 0 {
		t.Fatalf("tool output tokens missing: %#v", _signals)
	}
}
