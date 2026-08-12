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
