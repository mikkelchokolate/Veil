package observability

import "net/http"

type HTTPStatusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func NewHTTPStatusRecorder(w http.ResponseWriter) *HTTPStatusRecorder {
	return &HTTPStatusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (w *HTTPStatusRecorder) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *HTTPStatusRecorder) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *HTTPStatusRecorder) StatusCode() int { return w.statusCode }

// Unwrap exposes the wrapped writer so http.ResponseController can reach the
// underlying http.Flusher/Hijacker through middleware layers.
func (w *HTTPStatusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush forwards to the wrapped writer when it supports streaming (SSE).
func (w *HTTPStatusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
