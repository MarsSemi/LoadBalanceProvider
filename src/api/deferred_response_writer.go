package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// -------------------------------------------------------------------------------------
const deferredResponseBufferLimit = 2 * 1024 * 1024

// -------------------------------------------------------------------------------------
type deferredResponseWriter struct {
	target     http.ResponseWriter
	header     http.Header
	statusCode int
	buffer     bytes.Buffer
	stream     bool
	committed  bool
	writeErr   error
}

// -------------------------------------------------------------------------------------
func newDeferredResponseWriter(_target http.ResponseWriter, _stream bool) *deferredResponseWriter {
	return &deferredResponseWriter{target: _target, header: make(http.Header), stream: _stream}
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) Header() http.Header {
	return _w.header
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) WriteHeader(_statusCode int) {
	if _w.committed {
		return
	}
	if _w.statusCode == 0 {
		_w.statusCode = _statusCode
	}
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) Write(_data []byte) (int, error) {
	if _w.committed {
		return _w.target.Write(_data)
	}
	if _w.statusCode == 0 {
		_w.statusCode = http.StatusOK
	}
	_count, _err := _w.buffer.Write(_data)
	if _err != nil {
		return _count, _err
	}
	if _w.stream && _w.statusCode < http.StatusBadRequest && (streamBufferHasForwardableEvent(_w.buffer.Bytes()) || _w.buffer.Len() >= deferredResponseBufferLimit) {
		_w.writeErr = _w.Commit()
		if _w.writeErr != nil {
			return _count, _w.writeErr
		}
	}
	return _count, nil
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) Flush() {
	if !_w.committed {
		return
	}
	flushHTTPResponseWriter(_w.target)
}

// WriteStreamHeartbeat commits an idle SSE stream so the heartbeat reaches the
// client. Before this first heartbeat, provider failures remain replaceable.
// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) WriteStreamHeartbeat(_data []byte) error {
	if _w == nil || len(_data) == 0 {
		return nil
	}
	if _w.committed {
		_, _w.writeErr = _w.target.Write(_data)
		flushHTTPResponseWriter(_w.target)
		return _w.writeErr
	}
	if _w.statusCode == 0 {
		_w.statusCode = http.StatusOK
	}
	if _, _w.writeErr = _w.buffer.Write(_data); _w.writeErr != nil {
		return _w.writeErr
	}
	_w.writeErr = _w.Commit()
	return _w.writeErr
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) Commit() error {
	if _w == nil || _w.committed {
		return _w.writeErr
	}
	for _name, _values := range _w.header {
		_w.target.Header().Del(_name)
		for _, _value := range _values {
			_w.target.Header().Add(_name, _value)
		}
	}
	_statusCode := _w.statusCode
	if _statusCode == 0 {
		_statusCode = http.StatusOK
	}
	_w.target.WriteHeader(_statusCode)
	_w.committed = true
	if _w.buffer.Len() > 0 {
		_, _w.writeErr = io.Copy(_w.target, &_w.buffer)
	}
	flushHTTPResponseWriter(_w.target)
	return _w.writeErr
}

// -------------------------------------------------------------------------------------
// ResetForGracefulTerminal 丟棄尚未送出的緩衝內容，讓呼叫端改用一則正常完成的訊息收尾。
// 已經送出任何內容後不可使用（回傳 false）。
func (_w *deferredResponseWriter) ResetForGracefulTerminal() bool {
	if _w.committed {
		return false
	}
	_w.buffer.Reset()
	_w.statusCode = 0
	_w.header = make(http.Header)
	_w.writeErr = nil
	return true
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) Committed() bool {
	return _w != nil && _w.committed
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) StatusCode() int {
	if _w == nil || _w.statusCode == 0 {
		return http.StatusOK
	}
	return _w.statusCode
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) BufferedBody() string {
	if _w == nil {
		return ""
	}
	return _w.buffer.String()
}

// -------------------------------------------------------------------------------------
func (_w *deferredResponseWriter) HasBufferedResponse() bool {
	return _w != nil && (_w.buffer.Len() > 0 || _w.statusCode >= http.StatusBadRequest)
}

// -------------------------------------------------------------------------------------
func flushHTTPResponseWriter(_writer http.ResponseWriter) {
	if _flusher, _ok := _writer.(http.Flusher); _ok {
		_flusher.Flush()
		return
	}
	_ = http.NewResponseController(_writer).Flush()
}

// -------------------------------------------------------------------------------------
func streamBufferHasForwardableEvent(_data []byte) bool {
	for _, _line := range strings.Split(string(_data), "\n") {
		_line = strings.TrimSpace(_line)
		if !strings.HasPrefix(_line, "data:") {
			continue
		}
		_payloadText := strings.TrimSpace(strings.TrimPrefix(_line, "data:"))
		if _payloadText == "" {
			continue
		}
		if _payloadText == "[DONE]" {
			return true
		}
		var _payload map[string]interface{}
		if _err := json.Unmarshal([]byte(_payloadText), &_payload); _err != nil {
			return true
		}
		if !streamPayloadIsFailure(_payload) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------------------
func streamPayloadIsFailure(_payload map[string]interface{}) bool {
	_type := strings.ToLower(strings.TrimSpace(stringValue(_payload["type"])))
	if _type == "error" || strings.HasSuffix(_type, ".failed") {
		return true
	}
	_, _hasError := _payload["error"]
	return _type == "" && _hasError
}
