package platform

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON centralizes the API response content type and encoding.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

// WriteError keeps every error response in the same shape.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorBody{
		Error: errorDetail{Code: code, Message: message},
	})
}
