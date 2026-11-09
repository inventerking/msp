package api

import (
	"net/http"

	"github.com/google/uuid"

	"mspengine/internal/kernel/httpx"
)

const maxBodyBytes = httpx.MaxBodyBytes

func writeError(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func writeJSON(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func decode(w http.ResponseWriter, r *http.Request, into any) bool { return httpx.Decode(w, r, into) }

func pageParams(limit *int, cursor *string) (int32, *uuid.UUID, error) {
	return httpx.Page(limit, cursor)
}
