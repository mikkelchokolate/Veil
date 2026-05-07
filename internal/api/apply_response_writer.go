package api

import "net/http"

type ApplyResponseWriter struct{}

func NewApplyResponseWriter() ApplyResponseWriter { return ApplyResponseWriter{} }

func (ApplyResponseWriter) Write(w http.ResponseWriter, response ApplyResponse, status int, err error) {
	if err != nil {
		writeError(w, err.Error(), status)
		return
	}
	if status != http.StatusOK {
		writeJSONStatus(w, status, response)
		return
	}
	writeJSON(w, response)
}
