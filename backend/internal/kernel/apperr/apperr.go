// Package apperr defines errors with stable codes that are safe to show to clients.
package apperr

import "net/http"

// FieldError names a problem with one input field.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Error is safe to return to clients. Anything else is reported as internal_error.
type Error struct {
	Status       int
	Code         string
	Title        string
	Detail       string
	Fields       []FieldError
	ModuleKey    string
	ModuleStatus string
	RetryAfter   int
}

func (e *Error) Error() string { return e.Code }

func New(status int, code, title string) *Error {
	return &Error{Status: status, Code: code, Title: title}
}

func Validation(fields ...FieldError) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Title: "Some fields need attention", Fields: fields}
}

func NotFound() *Error { return New(http.StatusNotFound, "not_found", "Not found") }

func Unauthenticated() *Error {
	return New(http.StatusUnauthorized, "authentication_required", "Please sign in")
}

func Forbidden() *Error {
	return New(http.StatusForbidden, "insufficient_permission", "You do not have permission to do this")
}

func Conflict(code, title string) *Error { return New(http.StatusConflict, code, title) }

func InvalidCredentials() *Error {
	return New(http.StatusUnauthorized, "invalid_credentials", "Incorrect email or password")
}

func RateLimited(retryAfter int) *Error {
	e := New(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Please wait and try again")
	e.RetryAfter = retryAfter
	return e
}
