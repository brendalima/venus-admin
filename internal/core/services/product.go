package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
)

var _ ProductService = (*Product)(nil)

type Product struct {
	repo repository.ProductRepository
}

func NewProduct(repo repository.ProductRepository) (*Product, error) {
	if repo == nil {
		return nil, errors.New("missing product repository dependency")
	}
	return &Product{repo: repo}, nil
}

func MustNewProduct(repo repository.ProductRepository) *Product {
	p, err := NewProduct(repo)
	if err != nil {
		panic(fmt.Errorf("couldn't create product service: %w", err))
	}
	return p
}

func (s *Product) List(ctx context.Context) ([]domain.Product, error) {
	return s.repo.List(ctx)
}

func (s *Product) GetByID(ctx context.Context, id string) (domain.Product, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Product) Create(ctx context.Context, p domain.Product) (domain.Product, error) {
	p = p.Normalized()
	p.ID = uuid.New().String()
	p.Slug = domain.Slugify(p.Name)
	if err := p.Validate(); err != nil {
		return domain.Product{}, err
	}

	created, err := s.repo.Create(ctx, p)
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		// The slug comes from the name, so the name is what to change.
		return domain.Product{}, domain.NewValidationError("name", "Já existe uma peça com esse nome. Escolha outro.")
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't create product: %w", err)
	}
	return created, nil
}

func (s *Product) Update(ctx context.Context, p domain.Product) (domain.Product, error) {
	p = p.Normalized()
	// Typed by hand under "advanced options", so it is cleaned the same
	// way a name is: "Biquíni Novo" becomes "biquini-novo".
	p.Slug = domain.Slugify(p.Slug)
	if p.Slug == "" {
		p.Slug = domain.Slugify(p.Name)
	}
	if err := p.Validate(); err != nil {
		return domain.Product{}, err
	}

	current, err := s.repo.GetByID(ctx, p.ID)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", p.ID, err)
	}
	p.Position = current.Position

	updated, err := s.repo.Update(ctx, p)
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		return domain.Product{}, domain.NewValidationError("slug", "Já existe outra peça com esse endereço. Escolha outro.")
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't update product %s: %w", p.ID, err)
	}
	return updated, nil
}

func (s *Product) SetSoldOut(ctx context.Context, id string, soldOut bool) (domain.Product, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", id, err)
	}
	if p.SoldOut == soldOut {
		// Already as asked, as after a double tap: nothing to write, and
		// no change for the publish badge to report.
		return p, nil
	}
	p.SoldOut = soldOut

	updated, err := s.repo.Update(ctx, p)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't update product %s: %w", id, err)
	}
	return updated, nil
}

// Delete removes the product and its image rows. Products have no photo
// files yet; M2, which uploads them, must also remove them from storage.
func (s *Product) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("couldn't delete product %s: %w", id, err)
	}
	return nil
}
