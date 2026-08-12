package classifier

import (
	"context"
	"strings"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
type Classifier interface {
	Name() string
	Classify(_ctx context.Context, _req *domain.ChatCompletionRequest, _fallback domain.RequestProfile) (domain.RequestProfile, bool, error)
}

// -------------------------------------------------------------------------------------
func ShouldUseLLM(_profile domain.RequestProfile) bool {
	return len(_profile.Signals) == 0 || (_profile.ComplexityScore >= 4 && _profile.ComplexityScore <= 7)
}

// -------------------------------------------------------------------------------------
func ShouldAcceptLLM(_profile domain.RequestProfile, _confidence float64) bool {
	return IsValidTask(_profile.TaskType) && _confidence >= 0.6
}

// -------------------------------------------------------------------------------------
func IsValidTask(_taskType string) bool {
	_taskType = strings.ToLower(strings.TrimSpace(_taskType))
	_, _ok := validTasks()[_taskType]
	return _ok
}

// -------------------------------------------------------------------------------------
func validTasks() map[string]bool {
	return map[string]bool{
		"chat":             true,
		"coding":           true,
		"reasoning":        true,
		"summarization":    true,
		"translation":      true,
		"creative":         true,
		"extraction":       true,
		"vision":           true,
		"image_analysis":   true,
		"image_generation": true,
		"video_analysis":   true,
		"video_generation": true,
		"audio_analysis":   true,
		"transcription":    true,
		"audio_generation": true,
		"tts":              true,
	}
}

// -------------------------------------------------------------------------------------
