package benchmark

import (
	"bufio"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// -------------------------------------------------------------------------------------
const DefaultDataRoot = "questBank"

// -------------------------------------------------------------------------------------
var benchmarkFiles = map[string]string{
	"mmlu":          "mmlu.jsonl",
	"mmlu_pro":      "mmlu_pro.jsonl",
	"kmmlu":         "kmmlu.jsonl",
	"cmmlu":         "cmmlu.jsonl",
	"jmmlu":         "jmmlu.jsonl",
	"hellaswag":     "hellaswag.jsonl",
	"truthfulqa":    "truthfulqa.jsonl",
	"arc_challenge": "arc_challenge.jsonl",
	"winogrande":    "winogrande.jsonl",
	"gsm8k":         "gsm8k.jsonl",
	"mathqa":        "mathqa.jsonl",
	"humaneval":     "humaneval.jsonl",
	"mbpp":          "mbpp.jsonl",
	"livecodebench": "livecodebench.jsonl",
	"bbq":           "bbq.jsonl",
	"safetybench":   "safetybench.jsonl",
}

// -------------------------------------------------------------------------------------
func LoadQuestions(_root string, _benchmarkID string, _sampleSize int) ([]Question, error) {
	if _root == "" {
		_root = DefaultDataRoot
	}

	_questions, _err := loadQuestionsFromFile(_root, _benchmarkID)
	if _err != nil || len(_questions) == 0 {
		_questions = fallbackQuestions(_benchmarkID)
	}

	for _idx := range _questions {
		_questions[_idx].BenchmarkID = _benchmarkID
	}

	if _sampleSize <= 0 {
		return _questions, nil
	}
	if _sampleSize > len(_questions) {
		return expandQuestions(_questions, _sampleSize), nil
	}
	if _sampleSize == len(_questions) {
		return _questions, nil
	}
	return deterministicSample(_questions, _sampleSize), nil
}

// -------------------------------------------------------------------------------------
func expandQuestions(_questions []Question, _count int) []Question {
	if len(_questions) == 0 || _count <= len(_questions) {
		return _questions
	}

	_result := make([]Question, 0, _count)
	for len(_result) < _count {
		for _, _question := range _questions {
			_copy := _question
			if _copy.ID != "" {
				_copy.ID = _copy.ID + "#" + strconv.Itoa(len(_result)+1)
			}
			_result = append(_result, _copy)
			if len(_result) >= _count {
				break
			}
		}
	}
	return _result
}

// -------------------------------------------------------------------------------------
func loadQuestionsFromFile(_root string, _benchmarkID string) ([]Question, error) {
	_file, _ok := benchmarkFiles[_benchmarkID]
	if !_ok {
		return nil, os.ErrNotExist
	}

	_path := filepath.Join(resolveDataRoot(_root), _file)
	_handle, _err := os.Open(_path)
	if _err != nil {
		return nil, _err
	}
	defer _handle.Close()

	_questions := make([]Question, 0)
	_scanner := bufio.NewScanner(_handle)
	_buffer := make([]byte, 0, 1024*1024)
	_scanner.Buffer(_buffer, 16*1024*1024)
	for _scanner.Scan() {
		_line := strings.TrimSpace(_scanner.Text())
		if _line == "" {
			continue
		}
		var _raw map[string]interface{}
		if _err := json.Unmarshal([]byte(_line), &_raw); _err != nil {
			continue
		}
		if _question, _ok := normalizeQuestion(_benchmarkID, _raw); _ok {
			_questions = append(_questions, _question)
		}
	}
	return _questions, _scanner.Err()
}

// -------------------------------------------------------------------------------------
func resolveDataRoot(_root string) string {
	if filepath.IsAbs(_root) {
		return _root
	}
	if pathExists(_root) {
		return _root
	}

	_workDir, _err := os.Getwd()
	if _err != nil {
		return _root
	}
	for {
		_candidate := filepath.Join(_workDir, _root)
		if pathExists(_candidate) {
			return _candidate
		}
		_parent := filepath.Dir(_workDir)
		if _parent == _workDir {
			break
		}
		_workDir = _parent
	}
	return _root
}

// -------------------------------------------------------------------------------------
func pathExists(_path string) bool {
	_info, _err := os.Stat(_path)
	return _err == nil && _info.IsDir()
}

// -------------------------------------------------------------------------------------
func normalizeQuestion(_benchmarkID string, _raw map[string]interface{}) (Question, bool) {
	switch _benchmarkID {
	case "mmlu", "mmlu_pro", "kmmlu", "cmmlu", "jmmlu":
		_choices := stringSlice(_raw["choices"])
		_labels := stringSlice(_raw["labels"])
		if len(_labels) == 0 {
			_labels = defaultLabels(len(_choices))
		}
		_answer := answerString(_raw["answer"], _labels)
		return Question{ID: stringValue(_raw["id"]), Question: stringValue(_raw["question"]), Choices: _choices, Labels: _labels, Answer: _answer, Category: stringValue(_raw["subject"])}, len(_choices) > 0 && _answer != ""
	case "truthfulqa":
		return normalizeTruthfulQA(_raw)
	case "hellaswag":
		_choices := stringSlice(_raw["endings"])
		_answer := answerString(_raw["label"], defaultLabels(len(_choices)))
		return Question{ID: stringValue(_raw["ind"]), Context: stringValue(_raw["ctx"]), Choices: _choices, Labels: defaultLabels(len(_choices)), Answer: _answer, Category: stringValue(_raw["activity_label"])}, len(_choices) > 0 && _answer != ""
	case "arc_challenge", "mathqa", "bbq", "safetybench":
		_choices := stringSlice(_raw["choices"])
		_labels := stringSlice(_raw["labels"])
		if len(_labels) == 0 {
			_labels = defaultLabels(len(_choices))
		}
		return Question{ID: stringValue(_raw["id"]), Question: stringValue(_raw["question"]), Context: stringValue(_raw["context"]), Choices: _choices, Labels: _labels, Answer: strings.ToUpper(stringValue(_raw["answer"])), Category: stringValue(_raw["category"])}, len(_choices) > 0
	case "winogrande":
		return Question{ID: stringValue(_raw["id"]), Question: stringValue(_raw["sentence"]), Choices: []string{stringValue(_raw["option1"]), stringValue(_raw["option2"])}, Labels: []string{"1", "2"}, Answer: stringValue(_raw["answer"])}, true
	case "gsm8k":
		_answer := extractNumericAnswer(stringValue(_raw["answer"]))
		return Question{ID: stringValue(_raw["id"]), Question: stringValue(_raw["question"]), Answer: _answer}, _answer != ""
	case "humaneval":
		return Question{ID: stringValue(_raw["task_id"]), Prompt: stringValue(_raw["prompt"]), TestCode: stringValue(_raw["test"]), EntryPoint: stringValue(_raw["entry_point"]), Question: stringValue(_raw["prompt"])}, true
	case "mbpp":
		_tests := stringSlice(_raw["test_list"])
		return Question{ID: stringValue(_raw["task_id"]), Prompt: stringValue(_raw["prompt"]), TestCode: strings.Join(_tests, "\n"), Question: stringValue(_raw["prompt"])}, len(_tests) > 0
	case "livecodebench":
		return Question{ID: stringValue(_raw["question_id"]), Question: stringValue(_raw["question_content"]), Prompt: stringValue(_raw["question_content"]), StdinInput: firstPublicInput(_raw["public_test_cases"]), Stdout: firstPublicOutput(_raw["public_test_cases"])}, true
	default:
		return Question{}, false
	}
}

// -------------------------------------------------------------------------------------
func normalizeTruthfulQA(_raw map[string]interface{}) (Question, bool) {
	_targets, _ok := _raw["mc1_targets"].(map[string]interface{})
	if !_ok {
		return Question{}, false
	}
	_choices := stringSlice(_targets["choices"])
	_labelsRaw, _ := _targets["labels"].([]interface{})
	_answerIndex := -1
	for _idx, _value := range _labelsRaw {
		if intValue(_value) == 1 {
			_answerIndex = _idx
			break
		}
	}
	if _answerIndex < 0 || _answerIndex >= len(_choices) {
		return Question{}, false
	}
	_labels := defaultLabels(len(_choices))
	return Question{ID: stringValue(_raw["id"]), Question: stringValue(_raw["question"]), Choices: _choices, Labels: _labels, Answer: _labels[_answerIndex]}, true
}

// -------------------------------------------------------------------------------------
func deterministicSample(_questions []Question, _count int) []Question {
	_rng := rand.New(rand.NewSource(42))
	_indexes := _rng.Perm(len(_questions))
	_result := make([]Question, 0, _count)
	for _idx := 0; _idx < _count && _idx < len(_indexes); _idx++ {
		_result = append(_result, _questions[_indexes[_idx]])
	}
	return _result
}

// -------------------------------------------------------------------------------------
func stringValue(_value interface{}) string {
	switch _typed := _value.(type) {
	case string:
		return _typed
	case float64:
		return strconv.Itoa(int(_typed))
	case int:
		return strconv.Itoa(_typed)
	default:
		return ""
	}
}

// -------------------------------------------------------------------------------------
func intValue(_value interface{}) int {
	switch _typed := _value.(type) {
	case int:
		return _typed
	case float64:
		return int(_typed)
	case string:
		_int, _ := strconv.Atoi(_typed)
		return _int
	default:
		return 0
	}
}

// -------------------------------------------------------------------------------------
func stringSlice(_value interface{}) []string {
	_items, _ok := _value.([]interface{})
	if !_ok {
		if _strings, _ok := _value.([]string); _ok {
			return _strings
		}
		return nil
	}
	_result := make([]string, 0, len(_items))
	for _, _item := range _items {
		_result = append(_result, stringValue(_item))
	}
	return _result
}

// -------------------------------------------------------------------------------------
func firstPublicInput(_value interface{}) string {
	_case := firstPublicCase(_value)
	return stringValue(_case["input"])
}

// -------------------------------------------------------------------------------------
func firstPublicOutput(_value interface{}) string {
	_case := firstPublicCase(_value)
	return stringValue(_case["output"])
}

// -------------------------------------------------------------------------------------
func firstPublicCase(_value interface{}) map[string]interface{} {
	switch _typed := _value.(type) {
	case string:
		var _items []map[string]interface{}
		if _err := json.Unmarshal([]byte(_typed), &_items); _err == nil && len(_items) > 0 {
			return _items[0]
		}
	case []interface{}:
		if len(_typed) > 0 {
			_item, _ := _typed[0].(map[string]interface{})
			return _item
		}
	}
	return map[string]interface{}{}
}

// -------------------------------------------------------------------------------------
func defaultLabels(_count int) []string {
	_labels := make([]string, 0, _count)
	for _idx := 0; _idx < _count; _idx++ {
		_labels = append(_labels, string(rune('A'+_idx)))
	}
	return _labels
}

// -------------------------------------------------------------------------------------
func answerString(_value interface{}, _labels []string) string {
	switch _typed := _value.(type) {
	case string:
		if _idx, _err := strconv.Atoi(_typed); _err == nil && _idx >= 0 && _idx < len(_labels) {
			return _labels[_idx]
		}
		return strings.ToUpper(strings.TrimSpace(_typed))
	case float64:
		_idx := int(_typed)
		if _idx >= 0 && _idx < len(_labels) {
			return _labels[_idx]
		}
	}
	return ""
}
