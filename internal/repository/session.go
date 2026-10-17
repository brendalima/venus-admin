package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var _ SessionRepository = (*Session)(nil)

// Session stores the admin's login sessions for alexedwards/scs, which
// finds it through the scs.CtxStore method set.
//
// It replaces scs's own postgresstore, which ignores contexts and runs a
// cleanup query every five minutes: that is exactly Neon's suspend
// timeout, so the database would never get to sleep while the machine
// was up. Expired rows go when someone logs in instead.
type Session struct {
	db *sql.DB
}

func NewSession(db *sql.DB) (*Session, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Session{db: db}, nil
}

func MustNewSession(db *sql.DB) *Session {
	s, err := NewSession(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create session repository: %w", err))
	}
	return s
}

// FindCtx reports found = false for unknown and expired tokens alike, as
// scs requires; err is for infrastructure failures only.
func (r *Session) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	var data []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT data FROM sessions WHERE token = $1 AND now() < expiry`, token,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("couldn't find session: %w", err)
	}
	return data, true, nil
}

func (r *Session) CommitCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (token, data, expiry) VALUES ($1, $2, $3)
		 ON CONFLICT (token) DO UPDATE SET data = EXCLUDED.data, expiry = EXCLUDED.expiry`,
		token, data, expiry)
	if err != nil {
		return fmt.Errorf("couldn't save session: %w", err)
	}
	return nil
}

func (r *Session) DeleteCtx(ctx context.Context, token string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token); err != nil {
		return fmt.Errorf("couldn't delete session: %w", err)
	}
	return nil
}

func (r *Session) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expiry <= now()`)
	if err != nil {
		return 0, fmt.Errorf("couldn't delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("couldn't delete expired sessions: %w", err)
	}
	return n, nil
}

// Find, Commit and Delete complete scs.Store, which scs.CtxStore embeds.
// scs calls the Ctx variants whenever the store has them, so these exist
// only to satisfy the interface.

func (r *Session) Find(token string) ([]byte, bool, error) {
	return r.FindCtx(context.Background(), token)
}

func (r *Session) Commit(token string, data []byte, expiry time.Time) error {
	return r.CommitCtx(context.Background(), token, data, expiry)
}

func (r *Session) Delete(token string) error {
	return r.DeleteCtx(context.Background(), token)
}
