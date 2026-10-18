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
	store     storage.ImageStore
	processor storage.ImageProcessor
}

type ProductInput struct {
	Repo      repository.ProductRepository
	Store     storage.ImageStore
	Processor storage.ImageProcessor
}

func NewProduct(in ProductInput) (*Product, error) {
	switch {
	case in.Repo == nil:
		return nil, errors.New("missing product repository dependency")
	case in.Store == nil:
		return nil, errors.New("missing image store dependency")
	case in.Processor == nil:
		return nil, errors.New("missing image processor dependency")
	}
	return &Product{repo: in.Repo, store: in.Store, processor: in.Processor}, nil
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

func (s *Product) Create(ctx context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error) {
	p = p.Normalized()
	p.ID = uuid.New().String()
	p.Slug = domain.Slugify(p.Name)

	prepared, err := s.preparePhotos(ctx, nil, photos, p.Validate())
	if err != nil {
		return domain.Product{}, err
	}
	p.Images = prepared.images

	created, err := s.repo.Create(ctx, p)
	if err != nil {
		s.deleteFiles(ctx, prepared.stored...)
	}
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

func (s *Product) Update(ctx context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error) {
	p = p.Normalized()
	// Typed by hand under "advanced options", so it is cleaned the same
	// way a name is: "Biquíni Novo" becomes "biquini-novo".
	p.Slug = domain.Slugify(p.Slug)
	if p.Slug == "" {
		p.Slug = domain.Slugify(p.Name)
	}
	fieldErr := p.Validate()
	if fieldErr != nil && photos == nil {
		return domain.Product{}, fieldErr
	}

	current, err := s.repo.GetByID(ctx, p.ID)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", p.ID, err)
	}
	p.Position = current.Position

	var updated domain.Product
	var gone []domain.Image
	if photos == nil {
		updated, err = s.repo.Update(ctx, p)
	} else {
		var prepared preparedPhotos
		if prepared, err = s.preparePhotos(ctx, current.Images, photos, fieldErr); err != nil {
			return domain.Product{}, err
		}
		p.Images = prepared.images
		updated, gone, err = s.repo.UpdateWithImages(ctx, p, prepared.added, prepared.removed)
		if err != nil {
			s.deleteFiles(ctx, prepared.stored...)
		}
	}
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		return domain.Product{}, domain.NewValidationError("slug", "Já existe outra peça com esse endereço. Escolha outro.")
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't update product %s: %w", p.ID, err)
	}

	for _, img := range gone {
		s.deleteFiles(ctx, img.Key600, img.Key1200)
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

// preparedPhotos is a photo edit ready to be saved: the final list, and
// what changed.
type preparedPhotos struct {
	images  []domain.Image // in order, cover first
	added   []string       // IDs of the images in images that are new
	removed []string       // IDs of existing images to delete
	stored  []string       // keys of the files stored for the new ones
}

// preparePhotos checks edit against the photos the product has now,
// processes the new ones and stores their files. Every problem is
// reported at once, together with fieldErr, the product's own validation:
// one save tells the person everything to fix. Nothing is stored unless
// all of it is valid; once something is, the caller must delete stored if
// the save then fails.
func (s *Product) preparePhotos(ctx context.Context, current []domain.Image, edit *domain.PhotoEdit, fieldErr error) (preparedPhotos, error) {
	if edit == nil {
		return preparedPhotos{images: current}, fieldErr
	}
	e := *edit
	if e.AppendNew {
		e.Order = make([]domain.PhotoSlot, 0, len(current)+len(e.New))
		for _, img := range current {
			e.Order = append(e.Order, domain.PhotoSlot{ImageID: img.ID})
		}
		for i := range e.New {
			e.Order = append(e.Order, domain.PhotoSlot{NewIndex: i})
		}
		e.Removed, e.AppendNew = nil, false
	}
	if err := e.Validate(); err != nil {
		return preparedPhotos{}, errors.Join(fieldErr, err)
	}

	// Checked here too, not only in the repository's transaction, so a
	// stale screen is turned away before any photo is processed.
	byID := make(map[string]domain.Image, len(current))
	for _, img := range current {
		byID[img.ID] = img
	}
	mentioned := 0
	for _, slot := range e.Order {
		if slot.ImageID == "" {
			continue
		}
		if _, ok := byID[slot.ImageID]; !ok {
			return preparedPhotos{}, domain.NewStaleError("photos changed since they were loaded")
		}
		mentioned++
	}
	for _, id := range e.Removed {
		if _, ok := byID[id]; ok {
			mentioned++
		}
	}
	if mentioned != len(current) {
		return preparedPhotos{}, domain.NewStaleError("photos changed since they were loaded")
	}

	// One at a time, original read and dropped as it goes: a dozen phone
	// photos would not fit in memory together. Only the small results
	// are kept.
	type processed struct{ small, large []byte }
	results := make([]processed, len(e.New))
	var errs []error
	for i, src := range e.New {
		original, err := src.Bytes()
		if err != nil {
			return preparedPhotos{}, fmt.Errorf("couldn't read new photo %d: %w", i, err)
		}
		small, large, err := s.processor.Process(ctx, original)
		var invalid *domain.ValidationError
		if errors.As(err, &invalid) {
			errs = append(errs, domain.NewValidationError(domain.PhotoErrorField(i), invalid.Message))
			continue
		}
		if err != nil {
			return preparedPhotos{}, fmt.Errorf("couldn't process new photo %d: %w", i, err)
		}
		results[i] = processed{small: small, large: large}
	}
	if fieldErr != nil || len(errs) > 0 {
		return preparedPhotos{}, errors.Join(append([]error{fieldErr}, errs...)...)
	}

	newImages := make([]domain.Image, len(e.New))
	var stored []string
	for i, r := range results {
		id := uuid.New().String()
		img := domain.Image{ID: id, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg"}
		if err := s.store.Put(ctx, img.Key1200, r.large, imageContentType); err != nil {
			s.deleteFiles(ctx, stored...)
			return preparedPhotos{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
		}
		stored = append(stored, img.Key1200)
		if err := s.store.Put(ctx, img.Key600, r.small, imageContentType); err != nil {
			s.deleteFiles(ctx, stored...)
			return preparedPhotos{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
		}
		stored = append(stored, img.Key600)
		newImages[i] = img
	}

	out := preparedPhotos{removed: e.Removed, stored: stored, images: make([]domain.Image, len(e.Order))}
	for i, slot := range e.Order {
		if slot.ImageID != "" {
			out.images[i] = byID[slot.ImageID]
			continue
		}
		out.images[i] = newImages[slot.NewIndex]
		out.added = append(out.added, newImages[slot.NewIndex].ID)
	}
	return out, nil
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
