package benchmark

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// -------------------------------------------------------------------------------------
func maxTokensForBenchmark(_benchmarkID string) int {
	switch _benchmarkID {
	case "humaneval", "mbpp", "livecodebench":
		return 2048
	case "gsm8k":
		return 512
	default:
		return 128
	}
}

// -------------------------------------------------------------------------------------
func questionText(_question Question) string {
	if _question.Question != "" {
		return _question.Question
	}
	if _question.Context != "" {
		return _question.Context
	}
	return _question.Prompt
}

// -------------------------------------------------------------------------------------
func stripThink(_text string) string {
	_re := regexp.MustCompile(`(?is)<think>.*?</think>`)
	return strings.TrimSpace(_re.ReplaceAllString(_text, ""))
}

// -------------------------------------------------------------------------------------
func extractCode(_response string) string {
	_patterns := []*regexp.Regexp{
		regexp.MustCompile("(?s)```python\\s*\\n(.*?)```"),
		regexp.MustCompile("(?s)```\\s*\\n(.*?)```"),
	}
	for _, _pattern := range _patterns {
		_matches := _pattern.FindAllStringSubmatch(_response, -1)
		if len(_matches) > 0 {
			return strings.TrimSpace(_matches[len(_matches)-1][1])
		}
	}

	_lines := strings.Split(strings.TrimSpace(_response), "\n")
	_codeLines := make([]string, 0, len(_lines))
	_inCode := false
	for _, _line := range _lines {
		_trimmed := strings.TrimSpace(_line)
		if !_inCode && (strings.HasPrefix(_trimmed, "def ") || strings.HasPrefix(_trimmed, "class ") || strings.HasPrefix(_trimmed, "import ") || strings.HasPrefix(_trimmed, "from ")) {
			_inCode = true
		}
		if _inCode {
			_codeLines = append(_codeLines, _line)
		}
	}
	if len(_codeLines) > 0 {
		return strings.Join(_codeLines, "\n")
	}
	return strings.TrimSpace(_response)
}

// -------------------------------------------------------------------------------------
func runPythonFunctionTests(_code string, _testCode string, _entryPoint string) bool {
	if strings.TrimSpace(_entryPoint) == "" {
		return runPythonScript(_code + "\n" + _testCode)
	}
	return runPythonScript(_code + "\n\n" + _testCode + "\n\ncheck(" + _entryPoint + ")\n")
}

// -------------------------------------------------------------------------------------
func runPythonStdin(_code string, _stdin string, _expected string) bool {
	_output, _ok := runPython(_code, _stdin)
	if !_ok {
		return false
	}
	return strings.TrimSpace(_output) == strings.TrimSpace(_expected)
}

// -------------------------------------------------------------------------------------
func runPythonScript(_script string) bool {
	_, _ok := runPython(_script, "")
	return _ok
}

// -------------------------------------------------------------------------------------
func runPython(_script string, _stdin string) (string, bool) {
	_tmp, _err := os.CreateTemp("", "lbp-benchmark-*.py")
	if _err != nil {
		return "", false
	}
	_path := _tmp.Name()
	defer os.Remove(_path)
	if _, _err := _tmp.WriteString(_script); _err != nil {
		_ = _tmp.Close()
		return "", false
	}
	_ = _tmp.Close()

	_ctx, _cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer _cancel()

	_cmd := exec.CommandContext(_ctx, "python3", _path)
	if _stdin != "" {
		_cmd.Stdin = strings.NewReader(_stdin)
	}
	_output, _err := _cmd.CombinedOutput()
	return string(_output), _err == nil && _ctx.Err() == nil
}

// -------------------------------------------------------------------------------------
func containsLabel(_labels []string, _value string) bool {
	for _, _label := range _labels {
		if strings.EqualFold(strings.TrimSpace(_label), strings.TrimSpace(_value)) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func boolScore(_value bool) float64 {
	if _value {
		return 1
	}
	return 0
}

// -------------------------------------------------------------------------------------
func truncate(_value string, _limit int) string {
	if len(_value) <= _limit {
		return _value
	}
	return _value[:_limit]
}

// -------------------------------------------------------------------------------------
func defaultString(_value string, _fallback string) string {
	if strings.TrimSpace(_value) == "" {
		return _fallback
	}
	return _value
}

// -------------------------------------------------------------------------------------
func strconvParseFloat(_value string) (float64, error) {
	return strconv.ParseFloat(_value, 64)
}
