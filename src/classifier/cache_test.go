package classifier

import (
	"testing"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
func TestRequestCacheKeySeparatesTextAndImageContent(t *testing.T) {
	_textOnly := &domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "有收到圖片嗎？"},
		},
	}
	_withImagePart := &domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "有收到圖片嗎？"},
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo=", "detail": "auto"}},
			}},
		},
	}

	_textKey := RequestCacheKey(_textOnly)
	_imageKey := RequestCacheKey(_withImagePart)

	if _textKey == "" || _imageKey == "" {
		t.Fatalf("expected non-empty cache keys, got text=%q image=%q", _textKey, _imageKey)
	}
	if _textKey == _imageKey {
		t.Fatalf("text-only and image requests must not share cache key: %s", _textKey)
	}
}

// -------------------------------------------------------------------------------------
func TestRequestCacheKeySeparatesTextAndTopLevelAttachments(t *testing.T) {
	_textOnly := &domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "有收到圖片嗎？"},
		},
	}
	_withAttachment := &domain.ChatCompletionRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "有收到圖片嗎？"},
		},
		Attachments: []domain.ChatAttachment{
			{Name: "sample.png", MIMEType: "image/png", MediaType: "image", FileData: "iVBORw0KGgo="},
		},
	}

	_textKey := RequestCacheKey(_textOnly)
	_attachmentKey := RequestCacheKey(_withAttachment)

	if _textKey == "" || _attachmentKey == "" {
		t.Fatalf("expected non-empty cache keys, got text=%q attachment=%q", _textKey, _attachmentKey)
	}
	if _textKey == _attachmentKey {
		t.Fatalf("text-only and attachment requests must not share cache key: %s", _textKey)
	}
}
