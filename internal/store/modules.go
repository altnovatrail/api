package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// foreignKeyViolation is the SQLSTATE of a row that refers to one that is gone.
const foreignKeyViolation = "23503"

// ConnectedModule is a module connected to a profile (R07): its ID and when it
// was connected, with no version.
type ConnectedModule struct {
	ModuleID    string
	ConnectedAt time.Time
}

// Learner is what a learning request reads of its profile, in one query: the
// age category, by which a module's audience suits it (R07), and whether its
// consent is confirmed, as ConsentGiven says.
type Learner struct {
	AgeCategory string
	Consented   bool
}

// LearnerOf is the learner of the account's profile; ErrNoProfile when the
// account has no such profile.
func LearnerOf(ctx context.Context, db Querier, accountID, profileID string) (Learner, error) {
	var l Learner
	err := db.QueryRow(ctx, `SELECT p.age_category,
		EXISTS (SELECT 1 FROM consents c WHERE c.profile_id = p.id AND c.state = 'confirmed')
		FROM profiles p WHERE p.id = $1 AND p.account_id = $2`, profileID, accountID).Scan(&l.AgeCategory, &l.Consented)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Learner{}, ErrNoProfile
	case err != nil:
		return Learner{}, fmt.Errorf("store: read the learner: %w", err)
	}
	return l, nil
}

// ConnectModule connects the module to the profile, now; a module connected
// already keeps the time it was connected. A profile that is gone is
// ErrNoProfile: the row refers to it.
func ConnectModule(ctx context.Context, db Querier, profileID, moduleID string) error {
	_, err := db.Exec(ctx, `INSERT INTO connected_modules (profile_id, module_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, profileID, moduleID)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation:
		return ErrNoProfile
	case err != nil:
		return fmt.Errorf("store: connect the module: %w", err)
	}
	return nil
}

// DisconnectModule disconnects the module from the profile; a module not
// connected changes nothing.
func DisconnectModule(ctx context.Context, db Querier, profileID, moduleID string) error {
	if _, err := db.Exec(ctx, `DELETE FROM connected_modules WHERE profile_id = $1 AND module_id = $2`,
		profileID, moduleID); err != nil {
		return fmt.Errorf("store: disconnect the module: %w", err)
	}
	return nil
}

// ConnectedModules is the modules connected to the profile, in the order they
// were connected, and by ID among those connected at one time.
func ConnectedModules(ctx context.Context, db Lister, profileID string) ([]ConnectedModule, error) {
	rows, err := db.Query(ctx, `SELECT module_id, connected_at FROM connected_modules
		WHERE profile_id = $1 ORDER BY connected_at, module_id COLLATE "C"`, profileID)
	if err != nil {
		return nil, fmt.Errorf("store: read the connected modules: %w", err)
	}
	modules, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ConnectedModule])
	if err != nil {
		return nil, fmt.Errorf("store: read the connected modules: %w", err)
	}
	return modules, nil
}
