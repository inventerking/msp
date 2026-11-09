// Package httpx holds the response helpers every HTTP handler uses, so module handlers send
// errors in the same safe Problem Details shape as the platform.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
)

// MaxBodyBytes limits JSON bodies.
const MaxBodyBytes = 1 << 20

type problem struct {
	Type         string              `json:"type"`
	Title        string              `json:"title"`
	Status       int                 `json:"status"`
	Code         string              `json:"code"`
	Detail       string              `json:"detail,omitempty"`
	RequestID    string              `json:"request_id"`
	ModuleKey    string              `json:"module_key,omitempty"`
	ModuleStatus string              `json:"module_status,omitempty"`
	Errors       []apperr.FieldError `json:"errors,omitempty"`
}

// Error sends a safe problem response. Unknown errors are logged and hidden from the client.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	var e *apperr.Error
	if !errors.As(err, &e) {
		slog.ErrorContext(r.Context(), "request failed", "error", err, "request_id", authctx.MetaFrom(r.Context()).RequestID)
		e = apperr.New(http.StatusInternalServerError, "internal_error", "Something went wrong on our side")
	}
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(problem{
		Type: "about:blank", Title: e.Title, Status: e.Status, Code: e.Code, Detail: e.Detail,
		RequestID: authctx.MetaFrom(r.Context()).RequestID.String(),
		ModuleKey: e.ModuleKey, ModuleStatus: e.ModuleStatus, Errors: e.Fields,
	})
}

// JSON sends a JSON response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Decode reads a JSON body that the guard has already validated against the contract.
func Decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err := dec.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		Error(w, r, apperr.New(http.StatusBadRequest, "malformed_request", "The request could not be read"))
		return false
	}
	return true
}

// Page reads limit and cursor parameters. The contract bounds limit to 1-100.
func Page(limit *int, cursor *string) (int32, *uuid.UUID, error) {
	l := int32(25)
	if limit != nil {
		l = int32(*limit) //nolint:gosec // The contract limits this to 1-100.
	}
	if cursor == nil || *cursor == "" {
		return l, nil, nil
	}
	id, err := uuid.Parse(*cursor)
	if err != nil {
		return 0, nil, apperr.Validation(apperr.FieldError{Field: "cursor", Code: "invalid_cursor"})
	}
	return l, &id, nil
}

// Attachment serves bytes as a download that the browser will not render.
func Attachment(w http.ResponseWriter, name, contentType string, body []byte) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}
