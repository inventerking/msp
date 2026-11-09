package httpapi

import (
	"net/http"

	"mspengine/internal/kernel/apperr"
)

var (
	errMalformed = apperr.New(http.StatusBadRequest, "malformed_request", "The request could not be read")
	errAttest    = apperr.Validation(apperr.FieldError{Field: "lawful_source_attested", Code: "required"})
)
