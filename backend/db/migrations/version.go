package migrations

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Latest is the highest migration version in this build.
func Latest() (int64, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, e := range entries {
		head, _, ok := strings.Cut(e.Name(), "_")
		if !ok || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := strconv.ParseInt(head, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("migration %s has no version number", e.Name())
		}
		latest = max(latest, v)
	}
	return latest, nil
}

// CheckCurrent refuses to run against a database that is behind this build. Serving with an old
// schema would fail later, in confusing ways, on whichever request first touches a new table.
func CheckCurrent(ctx context.Context, pool *pgxpool.Pool) error {
	want, err := Latest()
	if err != nil {
		return err
	}
	var have int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&have); err != nil {
		return fmt.Errorf("cannot read the database schema version; run `engine migrate` first: %w", err)
	}
	if have < want {
		return fmt.Errorf("the database schema is at version %d but this build needs %d; run `engine migrate` first", have, want)
	}
	return nil
}
