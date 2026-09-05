package proxy

import (
	"errors"
	"net/http"
	"time"
)

// 優先保留各包裝層的行為；舊版 SDK 沒有 Unwrap 時，沿既有嵌入欄位尋找。
func responseWriterOperation(w http.ResponseWriter, operation func(http.ResponseWriter) error) error {
	for depth := 0; w != nil && depth < 64; depth++ {
		err := operation(w)
		if !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = unwrapper.Unwrap()
		} else {
			w = embeddedResponseWriter(w)
		}
	}
	return http.ErrNotSupported
}

func FlushResponseWriter(w http.ResponseWriter) error {
	return responseWriterOperation(w, func(current http.ResponseWriter) error {
		if flusher, ok := current.(interface{ FlushError() error }); ok {
			return flusher.FlushError()
		}
		if flusher, ok := current.(http.Flusher); ok {
			flusher.Flush()
			return nil
		}
		return http.ErrNotSupported
	})
}

func SetResponseWriteDeadline(w http.ResponseWriter, deadline time.Time) error {
	return responseWriterOperation(w, func(current http.ResponseWriter) error {
		if setter, ok := current.(interface{ SetWriteDeadline(time.Time) error }); ok {
			return setter.SetWriteDeadline(deadline)
		}
		return http.ErrNotSupported
	})
}
