package telemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode"
)

var (
	_uuidPattern       = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	_longHexPattern    = regexp.MustCompile(`(?i)\b[0-9a-f]{16,}\b`)
	_longNumberPattern = regexp.MustCompile(`\b\d{8,}\b`)
)

// RequestSignals 是由單次 Chat／Responses 請求取得的行為訊號。
// ToolCalls 只計入已回傳結果的工具，避免把模型提出但未實際執行的呼叫算進去。
type RequestSignals struct {
	Continuation     bool
	Fingerprint      string
	ToolCalls        int
	ToolRounds       int
	ToolOutputTokens int
}

// AnalyzeRequestJSON 同時支援 OpenAI Chat Completions 與 Responses 格式。
func AnalyzeRequestJSON(_body []byte) RequestSignals {
	_result := RequestSignals{}
	_decoder := json.NewDecoder(bytes.NewReader(_body))
	_decoder.UseNumber()
	_payload := map[string]interface{}{}
	if _err := _decoder.Decode(&_payload); _err != nil {
		return _result
	}

	if _messages, _ok := _payload["messages"].([]interface{}); _ok {
		analyzeChatMessages(_messages, &_result)
	} else {
		analyzeResponsesInput(_payload["input"], &_result)
	}

	if nonEmptyString(_payload["previous_response_id"]) || nonEmptyString(_payload["prompt_cache_key"]) {
		_result.Continuation = true
	}
	if _result.ToolCalls > 0 {
		_result.ToolRounds = 1
		_result.Continuation = true
	}
	return _result
}

func analyzeChatMessages(_messages []interface{}, _result *RequestSignals) {
	_userTexts := make([]string, 0)
	for _, _raw := range _messages {
		_message, _ok := _raw.(map[string]interface{})
		if !_ok {
			continue
		}
		_role := strings.ToLower(strings.TrimSpace(stringValue(_message["role"])))
		if _role == "user" {
			if _text := contentText(_message["content"]); _text != "" {
				_userTexts = append(_userTexts, _text)
			}
		}
	}
	if len(_userTexts) > 0 {
		_result.Fingerprint = promptFingerprint(_userTexts[len(_userTexts)-1])
	}
	if len(_userTexts) > 1 {
		_result.Continuation = true
	}

	for _idx := len(_messages) - 1; _idx >= 0; _idx-- {
		_message, _ok := _messages[_idx].(map[string]interface{})
		if !_ok {
			break
		}
		_role := strings.ToLower(strings.TrimSpace(stringValue(_message["role"])))
		if _role != "tool" && _role != "function" {
			break
		}
		noteToolResult(_result, _message["content"])
	}
}

func analyzeResponsesInput(_input interface{}, _result *RequestSignals) {
	if _text, _ok := _input.(string); _ok {
		_result.Fingerprint = promptFingerprint(_text)
		return
	}
	_items, _ok := _input.([]interface{})
	if !_ok {
		return
	}
	_userTexts := make([]string, 0)
	for _, _raw := range _items {
		_item, _ok := _raw.(map[string]interface{})
		if !_ok {
			continue
		}
		_type := strings.ToLower(strings.TrimSpace(stringValue(_item["type"])))
		_role := strings.ToLower(strings.TrimSpace(stringValue(_item["role"])))
		if _role == "user" || _type == "input_text" {
			if _text := contentText(firstNonNil(_item["content"], _item["text"])); _text != "" {
				_userTexts = append(_userTexts, _text)
			}
		}
	}
	if len(_userTexts) > 0 {
		_result.Fingerprint = promptFingerprint(_userTexts[len(_userTexts)-1])
	}
	if len(_userTexts) > 1 {
		_result.Continuation = true
	}

	for _idx := len(_items) - 1; _idx >= 0; _idx-- {
		_item, _ok := _items[_idx].(map[string]interface{})
		if !_ok {
			break
		}
		_type := strings.ToLower(strings.TrimSpace(stringValue(_item["type"])))
		if !strings.HasSuffix(_type, "_call_output") && _type != "function_call_output" {
			break
		}
		noteToolResult(_result, firstNonNil(_item["output"], _item["content"]))
	}
}

func noteToolResult(_result *RequestSignals, _output interface{}) {
	if _result == nil {
		return
	}
	_result.ToolCalls++
	_result.ToolOutputTokens += estimateTokens(contentText(_output))
}

func promptFingerprint(_text string) string {
	_text = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(_text)), " "))
	_text = _uuidPattern.ReplaceAllString(_text, "<id>")
	_text = _longHexPattern.ReplaceAllString(_text, "<id>")
	_text = _longNumberPattern.ReplaceAllString(_text, "<n>")
	if len(_text) > 8192 {
		_text = _text[:8192]
	}
	if _text == "" {
		return ""
	}
	_sum := sha256.Sum256([]byte(_text))
	return hex.EncodeToString(_sum[:])
}

func estimateTokens(_text string) int {
	if strings.TrimSpace(_text) == "" {
		return 0
	}
	_han := 0
	_other := 0
	for _, _rune := range _text {
		if unicode.Is(unicode.Han, _rune) {
			_han++
		} else {
			_other++
		}
	}
	return int(math.Ceil(float64(_han)/1.5 + float64(_other)/4.0))
}

func contentText(_value interface{}) string {
	switch _typed := _value.(type) {
	case nil:
		return ""
	case string:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(_typed)), "data:image/") ||
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(_typed)), "data:audio/") {
			return ""
		}
		return _typed
	case []interface{}:
		_parts := make([]string, 0, len(_typed))
		for _, _item := range _typed {
			if _text := contentText(_item); _text != "" {
				_parts = append(_parts, _text)
			}
		}
		return strings.Join(_parts, " ")
	case map[string]interface{}:
		_type := strings.ToLower(strings.TrimSpace(stringValue(_typed["type"])))
		if containsAny(_type, "image", "audio", "screenshot") {
			return ""
		}
		for _, _key := range []string{"text", "output_text", "content", "value", "output"} {
			if _text := contentText(_typed[_key]); _text != "" {
				return _text
			}
		}
		_encoded, _ := json.Marshal(_typed)
		return string(_encoded)
	default:
		_encoded, _ := json.Marshal(_typed)
		return string(_encoded)
	}
}

func containsAny(_value string, _needles ...string) bool {
	for _, _needle := range _needles {
		if strings.Contains(_value, _needle) {
			return true
		}
	}
	return false
}

func nonEmptyString(_value interface{}) bool {
	return strings.TrimSpace(stringValue(_value)) != ""
}

func stringValue(_value interface{}) string {
	_text, _ := _value.(string)
	return _text
}

func firstNonNil(_values ...interface{}) interface{} {
	for _, _value := range _values {
		if _value != nil {
			return _value
		}
	}
	return nil
}
