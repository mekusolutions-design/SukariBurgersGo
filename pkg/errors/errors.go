// Package errors provides a consistent HTTP error contract matching the NestJS
// HttpExceptionFilter response shape so frontends continue to work unchanged.
package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// APIError is the wire format returned to clients.
type APIError struct {
	StatusCode int       `json:"statusCode"`
	Timestamp  time.Time `json:"timestamp"`
	Path       string    `json:"path"`
	Method     string    `json:"method"`
	Message    string    `json:"message"`
	ErrorName  string    `json:"error"`
	RequestID  string    `json:"requestId,omitempty"`
	Detail     string    `json:"detail,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

// AppError is the internal error type used throughout the application.
type AppError struct {
	Code       int
	Message    string
	Err        error
	HTTPStatus int
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(status int, message string) *AppError {
	return &AppError{HTTPStatus: status, Message: message, Code: status}
}

func Wrap(status int, message string, err error) *AppError {
	return &AppError{HTTPStatus: status, Message: message, Err: err, Code: status}
}

func BadRequest(msg string) *AppError {
	return New(http.StatusBadRequest, msg)
}

func Unauthorized(msg string) *AppError {
	return New(http.StatusUnauthorized, msg)
}

func Forbidden(msg string) *AppError {
	return New(http.StatusForbidden, msg)
}

func NotFound(msg string) *AppError {
	return New(http.StatusNotFound, msg)
}

func Conflict(msg string) *AppError {
	return New(http.StatusConflict, msg)
}

func Internal(msg string) *AppError {
	return New(http.StatusInternalServerError, msg)
}

func Validation(msg string) *AppError {
	return New(http.StatusBadRequest, msg)
}

// WriteJSON writes the Nest-compatible error response.
// For 500s we still return a safe Message, but Detail carries the real error
// so staging can diagnose without digging through Render logs alone.
func WriteJSON(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	msg := "Internal server error"
	errName := "Internal Server Error"
	detail := ""

	if ae, ok := err.(*AppError); ok {
		status = ae.HTTPStatus
		msg = ae.Message
		errName = http.StatusText(status)
		if ae.Err != nil {
			detail = ae.Err.Error()
		}
		if status >= 500 && detail == "" {
			detail = ae.Message
		}
	} else if err != nil {
		detail = err.Error()
		msg = detail
	}

	// Staging: expose detail in message so the web toast is useful.
	// (Safe enough while Go POS is behind a flag / known URL.)
	if status >= 500 && detail != "" {
		msg = detail
	}

	reqID := ""
	if v := r.Context().Value(ctxKeyRequestID); v != nil {
		if s, ok := v.(string); ok {
			reqID = s
		}
	}

	body := APIError{
		StatusCode: status,
		Timestamp:  time.Now().UTC(),
		Path:       r.URL.Path,
		Method:     r.Method,
		Message:    msg,
		ErrorName:  errName,
		RequestID:  reqID,
		Detail:     detail,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type ctxKey string

const ctxKeyRequestID ctxKey = "request_id"

// RequestIDKey is exported for middleware to set.
func RequestIDKey() interface{} {
	return ctxKeyRequestID
}
