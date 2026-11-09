package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Lister is what a list is read from: a pool, a connection or a transaction.
type Lister interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// CountryAccepted says whether the deployment takes learners from country
// (R118, R119 spec; R36). None is taken until the operator adds it.
func CountryAccepted(ctx context.Context, db Querier, country string) (bool, error) {
	var accepted bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accepted_countries WHERE country = $1)`, country).Scan(&accepted)
	if err != nil {
		return false, fmt.Errorf("store: find the country: %w", err)
	}
	return accepted, nil
}

// AllowCountry adds country to the list, and says whether it was not there.
func AllowCountry(ctx context.Context, db Querier, country string) (bool, error) {
	tag, err := db.Exec(ctx, `INSERT INTO accepted_countries (country) VALUES ($1) ON CONFLICT DO NOTHING`, country)
	if err != nil {
		return false, fmt.Errorf("store: add the country: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RemoveCountry takes country off the list, and says whether it was there. The
// profiles of the country stay: a new one is refused.
func RemoveCountry(ctx context.Context, db Querier, country string) (bool, error) {
	tag, err := db.Exec(ctx, `DELETE FROM accepted_countries WHERE country = $1`, country)
	if err != nil {
		return false, fmt.Errorf("store: remove the country: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Countries is the list, by code.
func Countries(ctx context.Context, db Lister) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT country FROM accepted_countries ORDER BY country`)
	if err != nil {
		return nil, fmt.Errorf("store: read the countries: %w", err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: read the countries: %w", err)
	}
	return list, nil
}
