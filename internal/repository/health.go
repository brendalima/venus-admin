// Package repository is the persistence adapter: SQL over database/sql,
// translating infrastructure errors into domain errors.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var _ HealthRepository = (*Health)(nil)

type Health struct {
	db *sql.DB
}

func NewHealth(db *sql.DB) (*Health, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Health{db: db}, nil
}

func MustNewHealth(db *sql.DB) *Health {
	h, err := NewHealth(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create health repository: %w", err))
	}
	return h
}

func (h *Health) Ping(ctx context.Context) error {
	return h.db.PingContext(ctx)
}
