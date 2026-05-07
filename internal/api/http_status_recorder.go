package api

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
