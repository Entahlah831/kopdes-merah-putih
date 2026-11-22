package response

import (
	"encoding/json"
	"net/http"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data interface{}, meta ...interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	res := APIResponse{
		Success: true,
		Data:    data,
	}
	if len(meta) > 0 {
		res.Meta = meta[0]
	}
	_ = json.NewEncoder(w).Encode(res)
}

func Error(w http.ResponseWriter, status int, code, message string, details ...interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	apiErr := &APIError{
		Code:    code,
		Message: message,
	}
	if len(details) > 0 {
		apiErr.Details = details[0]
	}

	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error:   apiErr,
	})
}

func BadRequest(w http.ResponseWriter, message string, details ...interface{}) {
	Error(w, http.StatusBadRequest, "BAD_REQUEST", message, details...)
}

func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, "UNAUTHORIZED", message)
}

func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, "FORBIDDEN", message)
}

func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, "NOT_FOUND", message)
}

func Conflict(w http.ResponseWriter, message string, details ...interface{}) {
	Error(w, http.StatusConflict, "CONFLICT", message, details...)
}

func InternalServerError(w http.ResponseWriter, message string) {
	Error(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", message)
}
