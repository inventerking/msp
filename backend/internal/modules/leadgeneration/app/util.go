package app

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func containsCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}
