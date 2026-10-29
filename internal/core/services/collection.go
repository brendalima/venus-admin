package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
)

var _ CollectionService = (*Collection)(nil)

type Collection struct {
	repo repository.CollectionRepository
}

func NewCollection(repo repository.CollectionRepository) (*Collection, error) {
	if repo == nil {
		return nil, errors.New("missing collection repository dependency")
	}
	return &Collection{repo: repo}, nil
}

func MustNewCollection(repo repository.CollectionRepository) *Collection {
	c, err := NewCollection(repo)
	if err != nil {
		panic(fmt.Errorf("couldn't create collection service: %w", err))
	}
	return c
}

func (s *Collection) List(ctx context.Context) ([]domain.Collection, error) {
	return s.repo.List(ctx)
}

func (s *Collection) GetByID(ctx context.Context, id string) (domain.Collection, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Collection) Create(ctx context.Context, name string) (domain.Collection, error) {
	c := domain.Collection{ID: uuid.New().String(), Name: name}.Normalized()
	if err := c.Validate(); err != nil {
		return domain.Collection{}, err
	}
	created, err := s.repo.Create(ctx, c)
	if err != nil {
		return domain.Collection{}, collectionSaveError(err, "couldn't create collection")
	}
	return created, nil
}

func (s *Collection) Rename(ctx context.Context, id, name string) (domain.Collection, error) {
	c := domain.Collection{ID: id, Name: name}.Normalized()
	if err := c.Validate(); err != nil {
		return domain.Collection{}, err
	}
	renamed, err := s.repo.Rename(ctx, id, c.Name)
	if err != nil {
		return domain.Collection{}, collectionSaveError(err, "couldn't rename collection "+id)
	}
	return renamed, nil
}

func (s *Collection) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// collectionSaveError turns a name already in use into a message on the
// name, and wraps anything else with action.
func collectionSaveError(err error, action string) error {
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) && conflict.Subject == domain.ConflictCollection {
		return domain.NewValidationError("name", "Já existe uma coleção com esse nome")
	}
	return fmt.Errorf("%s: %w", action, err)
}
