package analyzer

import (
	"encoding/json"
	"strings"
	"testing"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func TestAnalyzeMarksImageAttachmentAsVisionRequirement(t *testing.T) {
	_analyzer := New()
	_profile := _analyzer.Analyze(&domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "請分析這張圖片"},
		},
		Attachments: []domain.ChatAttachment{
			{Name: "sample.png", MIMEType: "image/png", MediaType: "image", FileData: "iVBORw0KGgo="},
		},
	})

	if _profile.TaskType != "vision" {
		t.Fatalf("task type = %s, want vision", _profile.TaskType)
	}
	if !containsString(_profile.HardRequirements, "vision") {
		t.Fatalf("hard requirements = %#v, want vision", _profile.HardRequirements)
	}
}

// -------------------------------------------------------------------------------------
func TestAnalyzeMarksCamelCaseImageAttachmentAsVisionRequirement(t *testing.T) {
	var _request domain.ChatCompletionRequest
	_body := []byte(`{"model":"AUTO","messages":[{"role":"user","content":"請分析圖片"}],"attachments":[{"name":"sample.jpg","mimeType":"image/jpeg","mediaType":"image","fileData":"abc"}]}`)
	if _err := json.Unmarshal(_body, &_request); _err != nil {
		t.Fatalf("decode request: %v", _err)
	}

	_profile := New().Analyze(&_request)

	if _profile.TaskType != "vision" {
		t.Fatalf("task type = %s, want vision", _profile.TaskType)
	}
	if !containsString(_profile.HardRequirements, "vision") {
		t.Fatalf("hard requirements = %#v, want vision", _profile.HardRequirements)
	}
}

// -------------------------------------------------------------------------------------
func TestAnalyzeMarksDataURLAttachmentWithoutMetadataAsVisionRequirement(t *testing.T) {
	var _request domain.ChatCompletionRequest
	_body := []byte(`{"model":"AUTO","messages":[{"role":"user","content":"請分析圖片"}],"attachments":[{"content":"data:image/png;base64,iVBORw0KGgo="}]}`)
	if _err := json.Unmarshal(_body, &_request); _err != nil {
		t.Fatalf("decode request: %v", _err)
	}

	_profile := New().Analyze(&_request)

	if _profile.TaskType != "vision" {
		t.Fatalf("task type = %s, want vision", _profile.TaskType)
	}
	if !containsString(_profile.HardRequirements, "vision") {
		t.Fatalf("hard requirements = %#v, want vision", _profile.HardRequirements)
	}
}

// -------------------------------------------------------------------------------------
func TestAnalyzeDoesNotCountImageDataURLAsTextTokens(t *testing.T) {
	_dataURL := "data:image/png;base64," + strings.Repeat("A", 50000)
	_profile := New().Analyze(&domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": _dataURL},
			}},
		},
	})

	if _profile.TaskType != "vision" {
		t.Fatalf("task type = %s, want vision", _profile.TaskType)
	}
	if !containsString(_profile.HardRequirements, "vision") {
		t.Fatalf("hard requirements = %#v, want vision", _profile.HardRequirements)
	}
	if _profile.EstimatedInputTokens > 100 {
		t.Fatalf("estimated input tokens = %d, image payload should be redacted from routing text", _profile.EstimatedInputTokens)
	}
}

// -------------------------------------------------------------------------------------
func containsString(_items []string, _target string) bool {
	for _, _item := range _items {
		if _item == _target {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
// 短輸入 + 高推理程度實際上很耗配額，不能只看長度而判成低階請求。
func TestComplexityScoreAccountsForReasoningEffort(t *testing.T) {
	_analyzer := New()
	_short := "幫我看一下"

	_plain := _analyzer.ScoreComplexity(len([]rune(_short)), 512, 1, nil, "")
	_xhigh := _analyzer.ScoreComplexity(len([]rune(_short)), 512, 1, nil, "xhigh")
	if _xhigh <= _plain {
		t.Fatalf("xhigh effort should score higher: plain=%d xhigh=%d", _plain, _xhigh)
	}
	if _xhigh-_plain != 3 {
		t.Fatalf("xhigh should add 3 points, got %d", _xhigh-_plain)
	}
	if _high := _analyzer.ScoreComplexity(len([]rune(_short)), 512, 1, nil, "HIGH"); _high-_plain != 2 {
		t.Fatalf("high should add 2 points regardless of case, got %d", _high-_plain)
	}
}

// -------------------------------------------------------------------------------------
// 兩種寫法都要認得：頂層 reasoning_effort 與 Responses 的 reasoning.effort。
func TestAnalyzeReadsReasoningEffortFromBothShapes(t *testing.T) {
	_analyzer := New()
	_messages := []domain.ChatMessage{{Role: "user", Content: "hi"}}

	_flat := _analyzer.Analyze(&domain.ChatCompletionRequest{Messages: _messages, ReasoningEffort: "xhigh"})
	_nested := _analyzer.Analyze(&domain.ChatCompletionRequest{Messages: _messages, Reasoning: map[string]interface{}{"effort": "xhigh"}})
	_none := _analyzer.Analyze(&domain.ChatCompletionRequest{Messages: _messages})

	if _flat.ComplexityScore != _nested.ComplexityScore {
		t.Fatalf("both shapes should score the same: flat=%d nested=%d", _flat.ComplexityScore, _nested.ComplexityScore)
	}
	if _flat.ComplexityScore <= _none.ComplexityScore {
		t.Fatalf("effort should raise the score: with=%d without=%d", _flat.ComplexityScore, _none.ComplexityScore)
	}
}
