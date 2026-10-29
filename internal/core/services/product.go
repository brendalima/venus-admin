package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/storage"
)

var _ ProductService = (*Product)(nil)

// imageContentType is what the processor writes and the store serves.
const imageContentType = "image/jpeg"

type Product struct {
	repo        repository.ProductRepository
	collections repository.CollectionRepository
	store       storage.ImageStore
	processor   storage.ImageProcessor
}

type ProductInput struct {
	Repo repository.ProductRepository
	// Collections is asked whether the collection chosen for a piece
	// still exists.
	Collections repository.CollectionRepository
	Store       storage.ImageStore
	Processor   storage.ImageProcessor
}

func NewProduct(in ProductInput) (*Product, error) {
	switch {
	case in.Repo == nil:
		return nil, errors.New("missing product repository dependency")
	case in.Collections == nil:
		return nil, errors.New("missing collection repository dependency")
	case in.Store == nil:
		return nil, errors.New("missing image store dependency")
	case in.Processor == nil:
		return nil, errors.New("missing image processor dependency")
	}
	return &Product{repo: in.Repo, collections: in.Collections, store: in.Store, processor: in.Processor}, nil
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

func (s *Product) ListArchived(ctx context.Context) ([]domain.Product, error) {
	return s.repo.ListArchived(ctx)
}

func (s *Product) CountArchived(ctx context.Context) (int, error) {
	return s.repo.CountArchived(ctx)
}

func (s *Product) GetByID(ctx context.Context, id string) (domain.Product, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Product) Create(ctx context.Context, p domain.Product, colours domain.ColoursEdit) (domain.Product, error) {
	p = p.Normalized()
	p.ID = uuid.New().String()
	p.Slug = domain.Slugify(p.Name)

	fieldErr, err := s.validate(ctx, p)
	if err != nil {
		return domain.Product{}, err
	}
	prepared, err := s.prepareColours(ctx, nil, colours, p.Sizes, fieldErr)
	if err != nil {
		return domain.Product{}, err
	}
	p.Colours = prepared.colours

	created, err := s.repo.Create(ctx, p)
	if err != nil {
		s.deleteFiles(ctx, prepared.stored...)
		// The slug comes from the name, so the name is what to change.
		return domain.Product{}, saveError(err, "name", "Já existe uma peça com esse nome. Escolha outro.", "couldn't create product")
	}
	return created, nil
}

func (s *Product) Update(ctx context.Context, p domain.Product, colours *domain.ColoursEdit) (domain.Product, error) {
	p = p.Normalized()
	// Typed by hand under "advanced options", so it is cleaned the same
	// way a name is: "Biquíni Novo" becomes "biquini-novo".
	p.Slug = domain.Slugify(p.Slug)
	if p.Slug == "" {
		p.Slug = domain.Slugify(p.Name)
	}
	fieldErr, err := s.validate(ctx, p)
	if err != nil {
		return domain.Product{}, err
	}
	if fieldErr != nil && colours == nil {
		return domain.Product{}, fieldErr
	}

	current, err := s.repo.GetByID(ctx, p.ID)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", p.ID, err)
	}
	p.Position = current.Position

	var updated domain.Product
	var gone []domain.Image
	if colours == nil {
		updated, err = s.repo.Update(ctx, p)
	} else {
		var prepared preparedColours
		if prepared, err = s.prepareColours(ctx, current.Colours, *colours, p.Sizes, fieldErr); err != nil {
			return domain.Product{}, err
		}
		p.Colours = prepared.colours
		updated, gone, err = s.repo.UpdateWithColours(ctx, p, prepared.changes)
		if err != nil {
			s.deleteFiles(ctx, prepared.stored...)
		}
	}
	if err != nil {
		return domain.Product{}, saveError(err, "slug", "Já existe outra peça com esse endereço. Escolha outro.",
			"couldn't update product "+p.ID)
	}

	for _, img := range gone {
		s.deleteFiles(ctx, img.Key600, img.Key1200)
	}
	return updated, nil
}

// validate checks p's fields, and that its collection exists: it may
// have been deleted while the form was open. The problems with what was
// typed come first; err is a failure to check.
func (s *Product) validate(ctx context.Context, p domain.Product) (fieldErr, err error) {
	fieldErr = p.Validate()
	if p.CollectionID == "" {
		return fieldErr, nil
	}
	_, err = s.collections.GetByID(ctx, p.CollectionID)
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		return errors.Join(fieldErr, domain.NewValidationError("collection",
			"Essa coleção não existe mais. Escolha outra, ou deixe a peça sem coleção.")), nil
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't check collection %s: %w", p.CollectionID, err)
	}
	return fieldErr, nil
}

// saveError turns a uniqueness clash into a message on the field to
// change, slugField with slugMessage for the address, and wraps anything
// else with action.
func saveError(err error, slugField, slugMessage, action string) error {
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		switch conflict.Subject {
		case domain.ConflictSlug:
			return domain.NewValidationError(slugField, slugMessage)
		case domain.ConflictColourName:
			return domain.NewValidationError("colours", "Já existe outra cor com esse nome nesta peça")
		}
	}
	return fmt.Errorf("%s: %w", action, err)
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

func (s *Product) SetStock(ctx context.Context, productID, colourID, size string, quantity int32) (domain.Product, error) {
	p, err := s.repo.GetByID(ctx, productID)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", productID, err)
	}
	i := slices.IndexFunc(p.Colours, func(c domain.Colour) bool { return c.ID == colourID })
	if i < 0 {
		return domain.Product{}, domain.NewNotFoundError("colour not found")
	}
	// The size comes from a URL: only the product's own are counted.
	if !slices.Contains(p.Sizes, size) {
		return domain.Product{}, domain.NewValidationError("size", "Essa peça não tem o tamanho "+size)
	}
	switch {
	case quantity < 0:
		return domain.Product{}, domain.NewValidationError("quantity", "A quantidade não pode ser negativa")
	case quantity > domain.MaxQuantity:
		return domain.Product{}, domain.NewValidationError("quantity",
			fmt.Sprintf("A quantidade passa de %d. Confira o número.", domain.MaxQuantity))
	}
	if slices.Contains(p.Colours[i].Stock, domain.Stock{Size: size, Quantity: quantity}) {
		// Already as asked, as after a double tap: nothing to write, and
		// no change for the publish badge to report.
		return p, nil
	}

	if err := s.repo.SetStock(ctx, colourID, size, quantity); err != nil {
		return domain.Product{}, fmt.Errorf("couldn't set stock of colour %s: %w", colourID, err)
	}
	return s.repo.GetByID(ctx, productID)
}

func (s *Product) SetArchived(ctx context.Context, id string, archived bool) (domain.Product, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't load product %s: %w", id, err)
	}
	if p.Archived() == archived {
		// Already as asked: writing again would move the archive date and
		// light the publish badge for nothing.
		return p, nil
	}

	updated, err := s.repo.SetArchived(ctx, id, archived)
	if err != nil {
		return domain.Product{}, fmt.Errorf("couldn't set product %s archived: %w", id, err)
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
	for _, img := range p.Images() {
		s.deleteFiles(ctx, img.Key600, img.Key1200)
	}
	return nil
}

// preparedColours is a colour edit ready to be saved: the final list, and
// what changed.
type preparedColours struct {
	colours []domain.Colour // in order, each with its photos and stock
	changes domain.ColourChanges
	stored  []string // keys of the files stored for the new photos
}

// prepareColours checks edit against the colours the product has now,
// processes the new photos and stores their files. Every problem is
// reported at once, together with fieldErr, the product's own validation:
// one save tells the person everything to fix. Nothing is stored unless
// all of it is valid; once something is, the caller must delete stored if
// the save then fails.
func (s *Product) prepareColours(ctx context.Context, current []domain.Colour, edit domain.ColoursEdit, sizes []string, fieldErr error) (preparedColours, error) {
	e := edit.Normalized(sizes)
	byID := make(map[string]domain.Colour, len(current))
	for _, c := range current {
		byID[c.ID] = c
	}
	for n, c := range e.Colours {
		if c.Photos.AppendNew {
			e.Colours[n].Photos = appendNew(byID[c.ID].Images, c.Photos)
		}
	}
	if err := e.ValidateLists(); err != nil {
		return preparedColours{}, errors.Join(fieldErr, e.Validate(), err)
	}
	// Names and quantities are reported together with the photos'
	// problems, which takes processing them first.
	fieldErr = errors.Join(fieldErr, e.Validate())

	// Checked here too, not only in the repository's transaction, so a
	// stale screen is turned away before any photo is processed.
	stale := domain.NewStaleError("colours or photos changed since they were loaded")
	mentioned := 0
	for _, c := range e.Colours {
		if c.Blank() {
			continue
		}
		stored, exists := byID[c.ID]
		if c.ID != "" {
			if !exists {
				return preparedColours{}, stale
			}
			mentioned++
		}
		if !photosMatch(stored.Images, c.Photos) {
			return preparedColours{}, stale
		}
	}
	for _, id := range e.Removed {
		if _, ok := byID[id]; ok {
			mentioned++
		}
	}
	if mentioned != len(current) {
		return preparedColours{}, stale
	}

	// One at a time, original read and dropped as it goes: a dozen phone
	// photos would not fit in memory together. Only the small results
	// are kept.
	type processed struct{ small, large []byte }
	results := make([][]processed, len(e.Colours))
	var errs []error
	for n, c := range e.Colours {
		if c.Blank() {
			continue
		}
		results[n] = make([]processed, len(c.Photos.New))
		for i, src := range c.Photos.New {
			original, err := src.Bytes()
			if err != nil {
				return preparedColours{}, fmt.Errorf("couldn't read new photo %d of colour %d: %w", i, n, err)
			}
			small, large, err := s.processor.Process(ctx, original)
			var invalid *domain.ValidationError
			if errors.As(err, &invalid) {
				errs = append(errs, domain.NewValidationError(domain.ColourField(n, domain.PhotoErrorField(i)), invalid.Message))
				continue
			}
			if err != nil {
				return preparedColours{}, fmt.Errorf("couldn't process new photo %d of colour %d: %w", i, n, err)
			}
			results[n][i] = processed{small: small, large: large}
		}
	}
	if fieldErr != nil || len(errs) > 0 {
		return preparedColours{}, errors.Join(append([]error{fieldErr}, errs...)...)
	}

	out := preparedColours{colours: []domain.Colour{}}
	for n, c := range e.Colours {
		if c.Blank() {
			continue
		}
		colour := domain.Colour{ID: c.ID, Name: c.Name, Hexes: c.Hexes, Stock: c.Stock}
		if colour.ID == "" {
			colour.ID = uuid.New().String()
			out.changes.AddedColours = append(out.changes.AddedColours, colour.ID)
		}

		newImages := make([]domain.Image, len(results[n]))
		for i, r := range results[n] {
			id := uuid.New().String()
			img := domain.Image{ID: id, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg"}
			if err := s.store.Put(ctx, img.Key1200, r.large, imageContentType); err != nil {
				s.deleteFiles(ctx, out.stored...)
				return preparedColours{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
			}
			out.stored = append(out.stored, img.Key1200)
			if err := s.store.Put(ctx, img.Key600, r.small, imageContentType); err != nil {
				s.deleteFiles(ctx, out.stored...)
				return preparedColours{}, fmt.Errorf("couldn't store photo %s: %w", id, err)
			}
			out.stored = append(out.stored, img.Key600)
			newImages[i] = img
		}

		storedImages := map[string]domain.Image{}
		for _, img := range byID[c.ID].Images {
			storedImages[img.ID] = img
		}
		colour.Images = make([]domain.Image, len(c.Photos.Order))
		for i, slot := range c.Photos.Order {
			if slot.ImageID != "" {
				colour.Images[i] = storedImages[slot.ImageID]
				continue
			}
			colour.Images[i] = newImages[slot.NewIndex]
			out.changes.AddedImages = append(out.changes.AddedImages, newImages[slot.NewIndex].ID)
		}
		out.changes.RemovedImages = append(out.changes.RemovedImages, c.Photos.Removed...)
		out.colours = append(out.colours, colour)
	}
	out.changes.RemovedColours = e.Removed
	return out, nil
}

// appendNew resolves an AppendNew edit, which keeps the photos there are
// and adds the new ones after them, into the order it stands for.
func appendNew(current []domain.Image, e domain.PhotoEdit) domain.PhotoEdit {
	order := make([]domain.PhotoSlot, 0, len(current)+len(e.New))
	for _, img := range current {
		order = append(order, domain.PhotoSlot{ImageID: img.ID})
	}
	for i := range e.New {
		order = append(order, domain.PhotoSlot{NewIndex: i})
	}
	return domain.PhotoEdit{Order: order, New: e.New}
}

// photosMatch reports whether e accounts for exactly the photos stored,
// each kept or removed, and keeps none that is not.
func photosMatch(stored []domain.Image, e domain.PhotoEdit) bool {
	has := make(map[string]bool, len(stored))
	for _, img := range stored {
		has[img.ID] = true
	}
	mentioned := 0
	for _, slot := range e.Order {
		if slot.ImageID == "" {
			continue
		}
		if !has[slot.ImageID] {
			return false
		}
		mentioned++
	}
	for _, id := range e.Removed {
		if has[id] {
			mentioned++
		}
	}
	return mentioned == len(stored)
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
