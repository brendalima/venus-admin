package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/storage"
)

var _ ProductService = (*Product)(nil)

// imageContentType is what the processor writes and the store serves.
const imageContentType = "image/jpeg"

type Product struct {
	repo      repository.ProductRepository
	images    repository.ImageRepository
	store     storage.ImageStore
	processor storage.ImageProcessor
}

type ProductInput struct {
	Repo      repository.ProductRepository
	Images    repository.ImageRepository
	Store     storage.ImageStore
	Processor storage.ImageProcessor
}

func NewProduct(in ProductInput) (*Product, error) {
	switch {
	case in.Repo == nil:
		return nil, errors.New("missing product repository dependency")
	case in.Images == nil:
		return nil, errors.New("missing image repository dependency")
	case in.Store == nil:
		return nil, errors.New("missing image store dependency")
	case in.Processor == nil:
		return nil, errors.New("missing image processor dependency")
	}
	return &Product{repo: in.Repo, images: in.Images, store: in.Store, processor: in.Processor}, nil
}

func MustNewProduct(in ProductInput) *Product {
	p, err := NewProduct(in)
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

// Delete removes the product, then its photo files. The row goes first:
// a file left behind is wasted space, while a row left without its file
// would be a broken photo on the site.
func (s *Product) Delete(ctx context.Context, id string) error {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("couldn't load product %s: %w", id, err)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("couldn't delete product %s: %w", id, err)
	}
	for _, img := range p.Images {
		s.deleteFiles(ctx, img.Key600, img.Key1200)
	}
	return nil
}

func (s *Product) AddPhoto(ctx context.Context, productID string, original []byte) (domain.Image, error) {
	p, err := s.repo.GetByID(ctx, productID)
	if err != nil {
		return domain.Image{}, fmt.Errorf("couldn't load product %s: %w", productID, err)
	}
	if len(p.Images) >= domain.MaxImages {
		return domain.Image{}, domain.NewValidationError("photos", fmt.Sprintf(
			"Cada peça pode ter até %d fotos. Apague alguma antes de adicionar outra.", domain.MaxImages))
	}

	small, large, err := s.processor.Process(ctx, original)
	if err != nil {
		return domain.Image{}, err
	}

	id := uuid.New().String()
	img := domain.Image{
		ID:        id,
		ProductID: productID,
		Key600:    "produtos/" + id + "-600.jpg",
		Key1200:   "produtos/" + id + "-1200.jpg",
	}

	if err := s.store.Put(ctx, img.Key1200, large, imageContentType); err != nil {
		return domain.Image{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
	}
	if err := s.store.Put(ctx, img.Key600, small, imageContentType); err != nil {
		s.deleteFiles(ctx, img.Key1200)
		return domain.Image{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
	}

	added, err := s.images.Add(ctx, img)
	if err != nil {
		s.deleteFiles(ctx, img.Key600, img.Key1200)
		return domain.Image{}, fmt.Errorf("couldn't add photo %s to product %s: %w", id, productID, err)
	}
	return added, nil
}

func (s *Product) MovePhoto(ctx context.Context, productID, imageID string, index int) error {
	if err := s.images.Move(ctx, productID, imageID, index); err != nil {
		return fmt.Errorf("couldn't move photo %s: %w", imageID, err)
	}
	return nil
}

// DeletePhoto removes the row first, for the same reason as Delete.
func (s *Product) DeletePhoto(ctx context.Context, productID, imageID string) error {
	img, err := s.images.Delete(ctx, productID, imageID)
	if err != nil {
		return fmt.Errorf("couldn't delete photo %s: %w", imageID, err)
	}
	s.deleteFiles(ctx, img.Key600, img.Key1200)
	return nil
}

// deleteFiles removes files nothing points at any more. A failure leaves
// an orphan in the bucket, which costs a little space and harms nobody, so
// it is logged with the key for removing by hand rather than failing what
// the person asked for.
func (s *Product) deleteFiles(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if err := s.store.Delete(ctx, key); err != nil {
			slog.WarnContext(ctx, "couldn't delete photo file, left in storage", "key", key, "error", err)
		}
	}
}
