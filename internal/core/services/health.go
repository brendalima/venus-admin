// Package services holds the business rules and orchestrates the
// repositories and external adapters through their interfaces.
package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/brendalima/venus-admin/internal/repository"
)

var _ HealthService = (*Health)(nil)

type Health struct {
	repo repository.HealthRepository
}

func NewHealth(repo repository.HealthRepository) (*Health, error) {
	if repo == nil {
		return nil, errors.New("missing health repository dependency")
	}
	return &Health{repo: repo}, nil
}

func MustNewHealth(repo repository.HealthRepository) *Health {
	h, err := NewHealth(repo)
	if err != nil {
		panic(fmt.Errorf("couldn't create health service: %w", err))
	}
	return h
}

func (h *Health) Ready(ctx context.Context) error {
	if err := h.repo.Ping(ctx); err != nil {
		return fmt.Errorf("couldn't reach database: %w", err)
	}
	return nil
}
