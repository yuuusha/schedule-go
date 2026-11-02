package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Migrate(ctx context.Context, p *pgxpool.Pool, dir string) error {

	_, e := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	if e != nil {
		return e
	}

	es, e := os.ReadDir(dir)
	if e != nil {
		return e
	}

	var names []string
	for _, x := range es {
		if !x.IsDir() && strings.HasSuffix(x.Name(), ".sql") {
			names = append(names, x.Name())
		}
	}

	sort.Strings(names)

	for _, n := range names {
		var done bool

		e = p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, n).Scan(&done)
		if e != nil {
			return e
		}
		if done {
			continue
		}

		b, e := os.ReadFile(filepath.Join(dir, n))
		if e != nil {
			return e
		}

		tx, e := p.Begin(ctx)
		if e != nil {
			return e
		}

		_, e = tx.Exec(ctx, string(b))
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, n)
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", n, e)
		}

		e = tx.Commit(ctx)
		if e != nil {
			return e
		}
	}
	return nil
}
