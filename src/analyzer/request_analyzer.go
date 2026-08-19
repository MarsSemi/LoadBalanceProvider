package analyzer

import (
	"math"
	"strings"

	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
type RequestAnalyzer struct{}

// -------------------------------------------------------------------------------------
func New() *RequestAnalyzer {
	return &RequestAnalyzer{}
}

// -------------------------------------------------------------------------------------
func (_a *RequestAnalyzer) Analyze(_req *domain.ChatCompletionRequest) domain.RequestProfile {
	_text := _a.CombineMessages(_req.Messages)
	_inputChars := len([]rune(_text))
	_estimatedTokens := estimateTokens(_text)
	if _estimatedTokens < len(_req.Messages) {
		_estimatedTokens = len(_req.Messages)
	}

	_outputTokens := 1024
	if _req.MaxTokens != nil {
		_outputTokens = *_req.MaxTokens
	}
	if _req.MaxTokensV2 != nil {
		_outputTokens = *_req.MaxTokensV2
	}

	_hardRequirements := _a.HardRequirements(_req, _estimatedTokens)
	_taskType, _signals := _a.Classify(_text, _req.Messages)
	if _forcedTask := taskTypeFromHardRequirements(_hardRequirements); _forcedTask != "" {
		_taskType = _forcedTask
		_signals = append(_signals, _forcedTask)
	}
	_complexity := _a.ScoreComplexity(_estimatedTokens, _outputTokens, len(_req.Messages), _signals, requestReasoningEffort(_req))

	return domain.RequestProfile{
		InputCharacters:       _inputChars,
		EstimatedInputTokens:  _estimatedTokens,
		RequestedOutputTokens: _outputTokens,
		MessageCount:          len(_req.Messages),
		TaskType:              _taskType,
		ComplexityScore:       _complexity,
		Signals:               _signals,
		HardRequirements:      _hardRequirements,
	}
}

// -------------------------------------------------------------------------------------
func taskTypeFromHardRequirements(_requirements []string) string {
	for _, _requirement := range _requirements {
		switch strings.ToLower(strings.TrimSpace(_requirement)) {
		case "vision", "image_analysis":
			return "vision"
		case "image_generation", "image_edit", "image_variation":
			return "image_generation"
		case "audio_analysis", "transcription":
			return "transcription"
		case "audio_translation":
			return "translation"
		case "audio_generation", "tts":
			return "tts"
		case "video_analysis":
			return "video_analysis"
		case "video_generation":
			return "video_generation"
		}
	}
	return ""
}

// -------------------------------------------------------------------------------------
func (_a *RequestAnalyzer) CombineMessages(_messages []domain.ChatMessage) string {
	var _builder strings.Builder
	for _, _message := range _messages {
		_builder.WriteString(_message.Role)
		_builder.WriteString(": ")
		_builder.WriteString(_message.Text())
		_builder.WriteString("\n")
	}
	return _builder.String()
}

// -------------------------------------------------------------------------------------
func (_a *RequestAnalyzer) HardRequirements(_req *domain.ChatCompletionRequest, _estimatedTokens int) []string {
	_requirements := make([]string, 0)

	if hasVisionContent(_req.Messages) {
		_requirements = append(_requirements, "vision")
	}
	if hasAttachmentMedia(_req.Attachments, "image") {
		_requirements = append(_requirements, "vision")
	}
	if hasAudioContent(_req.Messages) {
		_requirements = append(_requirements, "audio_analysis")
	}
	if hasAttachmentMedia(_req.Attachments, "audio") {
		_requirements = append(_requirements, "audio_analysis")
	}
	if hasVideoContent(_req.Messages) {
		_requirements = append(_requirements, "video_analysis")
	}
	if hasAttachmentMedia(_req.Attachments, "video") {
		_requirements = append(_requirements, "video_analysis")
	}
	if len(_req.Tools) > 0 {
		_requirements = append(_requirements, "tools")
	}
	if responseFormatType(_req.ResponseFormat) == "json_object" {
		_requirements = append(_requirements, "json_mode")
	}
	if _estimatedTokens > 32000 {
		_requirements = append(_requirements, "long_context")
	}
	_requirements = append(_requirements, _req.RequiredCapabilities...)

	return uniqueStrings(_requirements)
}

// -------------------------------------------------------------------------------------
func (_a *RequestAnalyzer) Classify(_text string, _messages []domain.ChatMessage) (string, []string) {
	_lower := strings.ToLower(_text)
	_scores := map[string]int{
		"chat":          1,
		"coding":        0,
		"reasoning":     0,
		"summarization": 0,
		"translation":   0,
		"creative":      0,
		"extraction":    0,
	}

	_signalRules := map[string][]string{
		"coding":        {"```", "function ", "class ", "package ", "import ", "sql", "json schema", "debug", "refactor", "程式", "修正", "錯誤"},
		"reasoning":     {"分析", "推理", "規劃", "設計", "比較", "tradeoff", "why", "原因", "架構"},
		"summarization": {"摘要", "總結", "整理", "summarize", "summary", "重點"},
		"translation":   {"翻譯", "translate", "英文", "中文", "日文"},
		"creative":      {"創作", "文案", "故事", "creative", "行銷"},
		"extraction":    {"萃取", "抽取", "extract", "table", "csv", "欄位", "格式化"},
	}

	_signals := make([]string, 0)
	for _task, _patterns := range _signalRules {
		for _, _pattern := range _patterns {
			if strings.Contains(_lower, strings.ToLower(_pattern)) {
				_scores[_task]++
				_signals = append(_signals, _task)
				break
			}
		}
	}

	for _, _message := range _messages {
		if _message.Role == "system" {
			_scores["reasoning"]++
		}
	}

	_bestTask := "chat"
	_bestScore := _scores[_bestTask]
	for _task, _score := range _scores {
		if _score > _bestScore {
			_bestTask = _task
			_bestScore = _score
		}
	}

	return _bestTask, uniqueStrings(_signals)
}

// -------------------------------------------------------------------------------------
func (_a *RequestAnalyzer) ScoreComplexity(_inputTokens int, _outputTokens int, _messageCount int, _signals []string, _reasoningEffort string) int {
	_score := 1

	switch {
	case _inputTokens > 24000:
		_score += 5
	case _inputTokens > 12000:
		_score += 4
	case _inputTokens > 6000:
		_score += 3
	case _inputTokens > 2000:
		_score += 2
	case _inputTokens > 800:
		_score++
	}

	if _outputTokens > 4096 {
		_score += 2
	} else if _outputTokens > 2048 {
		_score++
	}

	if _messageCount > 12 {
		_score++
	}

	if len(_signals) >= 2 {
		_score++
	}

	// 高推理程度的請求即使輸入很短，實際消耗仍然很大；
	// 只看長度會把它誤判成低階請求。
	switch normalizeReasoningEffortForScore(_reasoningEffort) {
	case "xhigh":
		_score += 3
	case "high":
		_score += 2
	case "medium":
		_score++
	}

	if _score > 10 {
		_score = 10
	}

	return _score
}

// -------------------------------------------------------------------------------------
func estimateTokens(_text string) int {
	_chineseChars := 0
	_otherChars := 0
	for _, _rune := range _text {
		if isChinese(_rune) {
			_chineseChars++
			continue
		}
		_otherChars++
	}

	return int(math.Ceil(float64(_chineseChars)/1.5 + float64(_otherChars)/4.0))
}

// -------------------------------------------------------------------------------------
func isChinese(_rune rune) bool {
	return _rune >= '\u4e00' && _rune <= '\u9fff'
}

// -------------------------------------------------------------------------------------
func hasVisionContent(_messages []domain.ChatMessage) bool {
	for _, _message := range _messages {
		if contentHasImage(_message.Content) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func hasAudioContent(_messages []domain.ChatMessage) bool {
	for _, _message := range _messages {
		if contentHasMedia(_message.Content, "audio") {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func hasVideoContent(_messages []domain.ChatMessage) bool {
	for _, _message := range _messages {
		if contentHasMedia(_message.Content, "video") {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func hasAttachmentMedia(_attachments []domain.ChatAttachment, _mediaType string) bool {
	_mediaType = strings.ToLower(strings.TrimSpace(_mediaType))
	for _, _attachment := range _attachments {
		_declared := strings.ToLower(strings.TrimSpace(_attachment.MediaType))
		_mime := strings.ToLower(strings.TrimSpace(_attachment.MIMEType))
		_name := strings.ToLower(strings.TrimSpace(_attachment.Name))
		if _declared == _mediaType || strings.HasPrefix(_declared, _mediaType+"/") || strings.HasPrefix(_mime, _mediaType+"/") {
			return true
		}
		if attachmentPayloadHasMedia(_attachment, _mediaType) {
			return true
		}
		switch _mediaType {
		case "image":
			if strings.HasSuffix(_name, ".png") || strings.HasSuffix(_name, ".jpg") || strings.HasSuffix(_name, ".jpeg") || strings.HasSuffix(_name, ".webp") || strings.HasSuffix(_name, ".gif") {
				return true
			}
		case "audio":
			if strings.HasSuffix(_name, ".wav") || strings.HasSuffix(_name, ".mp3") || strings.HasSuffix(_name, ".m4a") || strings.HasSuffix(_name, ".webm") {
				return true
			}
		case "video":
			if strings.HasSuffix(_name, ".mp4") || strings.HasSuffix(_name, ".mov") || strings.HasSuffix(_name, ".webm") {
				return true
			}
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func attachmentPayloadHasMedia(_attachment domain.ChatAttachment, _mediaType string) bool {
	for _, _value := range []string{_attachment.Content, _attachment.FileData} {
		_value = strings.ToLower(strings.TrimSpace(_value))
		if _value == "" {
			continue
		}
		if strings.HasPrefix(_value, "data:"+_mediaType+"/") {
			return true
		}
		if strings.HasPrefix(_value, "http://") || strings.HasPrefix(_value, "https://") {
			switch _mediaType {
			case "image":
				if strings.Contains(_value, ".png") || strings.Contains(_value, ".jpg") || strings.Contains(_value, ".jpeg") || strings.Contains(_value, ".webp") || strings.Contains(_value, ".gif") {
					return true
				}
			case "audio":
				if strings.Contains(_value, ".wav") || strings.Contains(_value, ".mp3") || strings.Contains(_value, ".m4a") || strings.Contains(_value, ".webm") {
					return true
				}
			case "video":
				if strings.Contains(_value, ".mp4") || strings.Contains(_value, ".mov") || strings.Contains(_value, ".webm") {
					return true
				}
			}
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func contentHasImage(_content interface{}) bool {
	return contentHasMedia(_content, "image")
}

// -------------------------------------------------------------------------------------
func contentHasMedia(_content interface{}, _mediaType string) bool {
	_mediaType = strings.ToLower(strings.TrimSpace(_mediaType))
	if _mediaType == "" {
		return false
	}

	switch _value := _content.(type) {
	case string:
		_value = strings.ToLower(strings.TrimSpace(_value))
		return strings.Contains(_value, "data:"+_mediaType+"/")
	case []interface{}:
		for _, _part := range _value {
			if contentHasMedia(_part, _mediaType) {
				return true
			}
		}
	case map[string]interface{}:
		if _type, _ok := _value["type"].(string); _ok {
			_type = strings.ToLower(strings.TrimSpace(_type))
			if _type == _mediaType || strings.Contains(_type, _mediaType) {
				return true
			}
		}
		for _key := range _value {
			_key = strings.ToLower(strings.TrimSpace(_key))
			if _key == _mediaType || strings.Contains(_key, _mediaType+"_") || strings.Contains(_key, _mediaType) {
				return true
			}
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func responseFormatType(_responseFormat map[string]interface{}) string {
	if _responseFormat == nil {
		return ""
	}
	_value, _ := _responseFormat["type"].(string)
	return strings.ToLower(strings.TrimSpace(_value))
}

// -------------------------------------------------------------------------------------
func uniqueStrings(_values []string) []string {
	_seen := map[string]bool{}
	_result := make([]string, 0, len(_values))
	for _, _value := range _values {
		if _seen[_value] {
			continue
		}
		_seen[_value] = true
		_result = append(_result, _value)
	}
	return _result
}

// -------------------------------------------------------------------------------------
// requestReasoningEffort 取出請求指定的推理程度，兩種寫法都支援：
// 頂層 reasoning_effort，以及 Responses API 的 reasoning.effort。
func requestReasoningEffort(_req *domain.ChatCompletionRequest) string {
	if _req == nil {
		return ""
	}
	if _effort := strings.TrimSpace(_req.ReasoningEffort); _effort != "" {
		return _effort
	}
	if _req.Reasoning != nil {
		if _effort, _ok := _req.Reasoning["effort"].(string); _ok {
			return strings.TrimSpace(_effort)
		}
	}
	return ""
}

// -------------------------------------------------------------------------------------
func normalizeReasoningEffortForScore(_effort string) string {
	return strings.ToLower(strings.TrimSpace(_effort))
}
