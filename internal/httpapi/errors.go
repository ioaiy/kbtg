package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type APIError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

const (
	codeInvalidRequest      = "INVALID_REQUEST"
	codeInvalidAmount       = "INVALID_AMOUNT"
	codeInvalidUserID       = "INVALID_USER_ID"
	codeUserNotFound        = "USER_NOT_FOUND"
	codeInsufficientBalance = "INSUFFICIENT_BALANCE"
	codeUpstreamError       = "UPSTREAM_ERROR"
	codeInternalError       = "INTERNAL_ERROR"
)

func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, code, message string) {
	resp := APIError{
		Error:     message,
		Code:      code,
		RequestID: middleware.GetReqID(r.Context()),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("write error response failed", "err", err)
	}
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Error("write json response failed", "err", err)
	}
}
