package services

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockProductRepository struct {
	products map[string]domain.Product // by ID
	err      error                     // returned by every method when set
	writeErr error                     // returned by Create and Update when set
	writes   int
	changes  domain.ColourChanges // the last UpdateWithColours received
}

func newMockProductRepository(ps ...domain.Product) *mockProductRepository {
	m := &mockProductRepository{products: map[string]domain.Product{}}
	for _, p := range ps {
		// Tests change the stored colours; the fixtures must not change
		// with them.
		p.Colours = slices.Clone(p.Colours)
		m.products[p.ID] = p
	}
	return m
}

func (m *mockProductRepository) List(context.Context) ([]domain.Product, error) {
	if m.err != nil {
		return nil, m.err
	}
	ps := []domain.Product{}
	for _, p := range m.products {
		if !p.Archived() {
			ps = append(ps, p)
		}
	}
	return ps, nil
}

func (m *mockProductRepository) ListArchived(context.Context) ([]domain.Product, error) {
	if m.err != nil {
		return nil, m.err
	}
	ps := []domain.Product{}
	for _, p := range m.products {
		if p.Archived() {
			ps = append(ps, p)
		}
	}
	return ps, nil
}

func (m *mockProductRepository) CountArchived(ctx context.Context) (int, error) {
	ps, err := m.ListArchived(ctx)
	return len(ps), err
}

func (m *mockProductRepository) SetArchived(_ context.Context, id string, archived bool) (domain.Product, error) {
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	m.writes++
	p.ArchivedAt = time.Time{}
	if archived {
		p.ArchivedAt = time.Now()
	}
	m.products[id] = p
	return p, nil
}

func (m *mockProductRepository) GetByID(_ context.Context, id string) (domain.Product, error) {
	if m.err != nil {
		return domain.Product{}, m.err
	}
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	return p, nil
}

func (m *mockProductRepository) GetBySlug(context.Context, string) (domain.Product, error) {
	return domain.Product{}, errors.New("not used by the service")
}

func (m *mockProductRepository) Create(_ context.Context, p domain.Product) (domain.Product, error) {
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	m.writes++
	p.Position = int32(len(m.products) + 1)
	m.products[p.ID] = p
	return p, nil
}

func (m *mockProductRepository) Update(_ context.Context, p domain.Product) (domain.Product, error) {
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	m.writes++
	p.Colours = m.products[p.ID].Colours // a plain update leaves them alone
	m.products[p.ID] = p
	return p, nil
}

// UpdateWithColours trusts the service's own staleness check, which is
// what these tests exercise; the real one is covered by the repository's
// integration tests.
func (m *mockProductRepository) UpdateWithColours(_ context.Context, p domain.Product, changes domain.ColourChanges) (domain.Product, []domain.Image, error) {
	if m.writeErr != nil {
		return domain.Product{}, nil, m.writeErr
	}
	m.writes++
	m.changes = changes
	var gone []domain.Image
	for _, c := range m.products[p.ID].Colours {
		for _, img := range c.Images {
			if slices.Contains(changes.RemovedImages, img.ID) || slices.Contains(changes.RemovedColours, c.ID) {
				gone = append(gone, img)
			}
		}
	}
	m.products[p.ID] = p
	return p, gone, nil
}

func (m *mockProductRepository) SetStock(_ context.Context, colourID, size string, quantity int32) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	for id, p := range m.products {
		for i, c := range p.Colours {
			if c.ID != colourID {
				continue
			}
			m.writes++
			p.Colours = slices.Clone(p.Colours)
			stock := slices.DeleteFunc(slices.Clone(c.Stock), func(s domain.Stock) bool { return s.Size == size })
			p.Colours[i].Stock = append(stock, domain.Stock{Size: size, Quantity: quantity})
			m.products[id] = p
			return nil
		}
	}
	return domain.NewNotFoundError("colour not found")
}

func (m *mockProductRepository) Delete(_ context.Context, id string) error {
	if m.err != nil {
		return m.err
	}
	if _, ok := m.products[id]; !ok {
		return domain.NewNotFoundError("product not found")
	}
	delete(m.products, id)
	return nil
}

func validInput() domain.Product {
	return domain.Product{
		Name:       " Biquíni Aurora ",
		PriceCents: 18900,
		Sizes:      []string{"G", "P"},
		Details:    []string{"Verde", ""},
	}
}

// existing is a stored product the update tests start from, with one
// colour.
var existing = domain.Product{
	ID: "8a0e2f9e-4a8b-4c55-9d4c-6f1b7b0f2a11", Slug: "biquini-aurora", Name: "Biquíni Aurora",
	PriceCents: 18900, Sizes: []string{"P"}, Position: 7,
	Colours: []domain.Colour{{ID: "0c7d4a52-7f0a-4f38-9a53-5be0f0b3f6d2", Name: "Bege"}},
}

// oneColour is the simplest colour edit a new product can be created
// with.
func oneColour() domain.ColoursEdit {
	return domain.ColoursEdit{Colours: []domain.ColourEdit{{Name: "Bege"}}}
}

// keepColours is an edit that leaves the stored product's colours, photos
// and stock as they are.
func keepColours(p domain.Product) *domain.ColoursEdit {
	e := &domain.ColoursEdit{}
	for _, c := range p.Colours {
		e.Colours = append(e.Colours, domain.ColourEdit{
			ID: c.ID, Name: c.Name, Stock: c.Stock, Photos: domain.PhotoEdit{AppendNew: true},
		})
	}
	return e
}

func TestNewProduct(t *testing.T) {
	full := ProductInput{Repo: newMockProductRepository(), Collections: newMockCollectionRepository(), Store: newMockImageStore(), Processor: &mockProcessor{}}
	for name, in := range map[string]ProductInput{
		"repository":  {Collections: full.Collections, Store: full.Store, Processor: full.Processor},
		"collections": {Repo: full.Repo, Store: full.Store, Processor: full.Processor},
		"store":       {Repo: full.Repo, Collections: full.Collections, Processor: full.Processor},
		"processor":   {Repo: full.Repo, Collections: full.Collections, Store: full.Store},
	} {
		if _, err := NewProduct(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewProduct(full); err != nil {
		t.Errorf("complete input rejected: %v", err)
	}
}

func TestProductCreate(t *testing.T) {
	repo := newMockProductRepository()
	s := newTestProductService(repo)

	p, err := s.Create(context.Background(), validInput(), oneColour())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Colours) != 1 || p.Colours[0].Name != "Bege" || uuid.Validate(p.Colours[0].ID) != nil {
		t.Errorf("colours %+v, want Bege with a generated UUID", p.Colours)
	}
	if uuid.Validate(p.ID) != nil {
		t.Errorf("ID = %q, want a generated UUID", p.ID)
	}
	if p.Slug != "biquini-aurora" || p.Name != "Biquíni Aurora" {
		t.Errorf("slug %q, name %q", p.Slug, p.Name)
	}
	if !slices.Equal(p.Sizes, []string{"P", "G"}) || !slices.Equal(p.Details, []string{"Verde"}) {
		t.Errorf("sizes %v, details %v, want them normalised", p.Sizes, p.Details)
	}
}

func TestProductCollection(t *testing.T) {
	ctx := context.Background()
	repo := newMockProductRepository()
	collections := newMockCollectionRepository(domain.Collection{ID: "verao", Name: "Verão 2026"})
	s := MustNewProduct(ProductInput{Repo: repo, Collections: collections, Store: newMockImageStore(), Processor: &mockProcessor{}})

	in := validInput()
	in.CollectionID = " verao "
	p, err := s.Create(ctx, in, oneColour())
	if err != nil {
		t.Fatal(err)
	}
	if p.CollectionID != "verao" {
		t.Errorf("collection %q, want verao", p.CollectionID)
	}

	// Deleted while the form was open: a message on the field, and
	// nothing saved, together with the other fields' problems.
	writes := repo.writes
	in.CollectionID = "gone"
	in.Name = ""
	_, err = s.Create(ctx, in, oneColour())
	if got := fieldsOf(err); !slices.Contains(got, "collection") || !slices.Contains(got, "name") {
		t.Errorf("fields %v, want collection and name", got)
	}
	p.CollectionID = "gone"
	if _, err := s.Update(ctx, p, nil); !slices.Contains(fieldsOf(err), "collection") {
		t.Errorf("update: err = %v, want a validation error on collection", err)
	}
	if repo.writes != writes {
		t.Error("a piece was saved with a collection that does not exist")
	}

	// Out of a collection: nothing to check.
	p.CollectionID = ""
	collections.err = errors.New("database down")
	if _, err := s.Update(ctx, p, nil); err != nil {
		t.Errorf("update without a collection: %v", err)
	}
	// A failure to check is not the person's mistake.
	p.CollectionID = "verao"
	var invalid *domain.ValidationError
	if _, err := s.Update(ctx, p, nil); err == nil || errors.As(err, &invalid) {
		t.Errorf("update while the check fails: err = %v, want a plain error", err)
	}
}

func TestProductCreateFailures(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(*domain.Product)
		colours   *domain.ColoursEdit // oneColour when nil
		writeErr  error
		wantField string // validation error expected on this field
	}{
		{name: "invalid", edit: func(p *domain.Product) { p.Name, p.PriceCents = "", 0 }, wantField: "name"},
		{name: "no colour", colours: &domain.ColoursEdit{}, wantField: "colours"},
		{name: "name taken", writeErr: domain.NewConflictError(domain.ConflictSlug), wantField: "name"},
		{name: "colour name taken", writeErr: domain.NewConflictError(domain.ConflictColourName), wantField: "colours"},
		{name: "database down", writeErr: errors.New("timeout")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockProductRepository()
			repo.writeErr = tt.writeErr
			in := validInput()
			if tt.edit != nil {
				tt.edit(&in)
			}

			colours := oneColour()
			if tt.colours != nil {
				colours = *tt.colours
			}

			_, err := newTestProductService(repo).Create(context.Background(), in, colours)

			var v *domain.ValidationError
			if tt.wantField == "" {
				if err == nil || errors.As(err, &v) {
					t.Fatalf("err = %v, want an infrastructure error", err)
				}
				return
			}
			if !errors.As(err, &v) || v.Field != tt.wantField {
				t.Fatalf("err = %v, want a ValidationError on %s", err, tt.wantField)
			}
			if tt.writeErr == nil && repo.writes != 0 {
				t.Error("an invalid product reached the repository")
			}
		})
	}
}

func TestProductUpdate(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		wantSlug string
	}{
		// Renaming does not move the page: links already shared keep working.
		{"slug kept", "biquini-aurora", "biquini-aurora"},
		{"slug typed by hand", "Aurora Verão", "aurora-verao"},
		{"slug cleared", "", "biquini-aurora-verde"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockProductRepository(existing)
			in := validInput()
			in.ID = existing.ID
			in.Name = "Biquíni Aurora Verde"
			in.Slug = tt.slug
			in.Position = 99 // ignored

			p, err := newTestProductService(repo).Update(context.Background(), in, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", p.Slug, tt.wantSlug)
			}
			if p.Position != existing.Position {
				t.Errorf("position = %d, want %d kept", p.Position, existing.Position)
			}
		})
	}
}

func TestProductUpdateFailures(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		writeErr  error
		colours   *domain.ColoursEdit
		wantField string
		notFound  bool
	}{
		{name: "missing product", id: uuid.New().String(), notFound: true},
		{name: "slug taken", id: existing.ID, writeErr: domain.NewConflictError(domain.ConflictSlug), wantField: "slug"},
		{
			name: "colour name taken", id: existing.ID, colours: keepColours(existing),
			writeErr: domain.NewConflictError(domain.ConflictColourName), wantField: "colours",
		},
		{name: "database down", id: existing.ID, writeErr: errors.New("timeout")},
		{name: "database down with colours", id: existing.ID, colours: keepColours(existing), writeErr: errors.New("timeout")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockProductRepository(existing)
			repo.writeErr = tt.writeErr
			in := validInput()
			in.ID = tt.id

			_, err := newTestProductService(repo).Update(context.Background(), in, tt.colours)

			var v *domain.ValidationError
			var nf *domain.NotFoundError
			switch {
			case tt.notFound:
				if !errors.As(err, &nf) {
					t.Errorf("err = %v, want NotFoundError", err)
				}
			case tt.wantField != "":
				if !errors.As(err, &v) || v.Field != tt.wantField {
					t.Errorf("err = %v, want a ValidationError on %s", err, tt.wantField)
				}
			default:
				if err == nil || errors.As(err, &v) || errors.As(err, &nf) {
					t.Errorf("err = %v, want an infrastructure error", err)
				}
			}
		})
	}
}

func TestProductSetSoldOut(t *testing.T) {
	repo := newMockProductRepository(existing)
	s := newTestProductService(repo)

	p, err := s.SetSoldOut(context.Background(), existing.ID, true)
	if err != nil || !p.SoldOut {
		t.Fatalf("SetSoldOut = %+v, %v", p, err)
	}
	if _, err := s.SetSoldOut(context.Background(), existing.ID, true); err != nil {
		t.Fatal(err)
	}
	if repo.writes != 1 {
		t.Errorf("writes = %d, want 1: asking twice must not record a second change", repo.writes)
	}

	var nf *domain.NotFoundError
	if _, err := s.SetSoldOut(context.Background(), uuid.New().String(), true); !errors.As(err, &nf) {
		t.Errorf("missing product: err = %v, want NotFoundError", err)
	}
}

func TestProductSetStock(t *testing.T) {
	ctx := context.Background()
	colour := existing.Colours[0].ID

	repo := newMockProductRepository(existing)
	s := newTestProductService(repo)
	p, err := s.SetStock(ctx, existing.ID, colour, "P", 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.Stock{{Size: "P", Quantity: 3}}; !slices.Equal(p.Colours[0].Stock, want) {
		t.Errorf("stock %v, want %v", p.Colours[0].Stock, want)
	}
	if _, err := s.SetStock(ctx, existing.ID, colour, "P", 3); err != nil {
		t.Fatal(err)
	}
	if repo.writes != 1 {
		t.Errorf("writes = %d, want 1: asking twice must not record a second change", repo.writes)
	}
	if p, err = s.SetStock(ctx, existing.ID, colour, "P", 0); err != nil || p.Colours[0].Stock[0].Quantity != 0 {
		t.Errorf("down to zero = %+v, %v", p.Colours, err)
	}

	tests := []struct {
		name            string
		product, colour string
		size            string
		quantity        int32
		wantField       string
		wantNotFound    bool
	}{
		{name: "missing product", product: uuid.New().String(), colour: colour, size: "P", wantNotFound: true},
		{name: "colour of another product", product: existing.ID, colour: uuid.New().String(), size: "P", wantNotFound: true},
		// G is a size, but not one this piece comes in.
		{name: "size not in the grid", product: existing.ID, colour: colour, size: "G", wantField: "size"},
		{name: "negative", product: existing.ID, colour: colour, size: "P", quantity: -1, wantField: "quantity"},
		{name: "too many", product: existing.ID, colour: colour, size: "P", quantity: domain.MaxQuantity + 1, wantField: "quantity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockProductRepository(existing)
			_, err := newTestProductService(repo).SetStock(ctx, tt.product, tt.colour, tt.size, tt.quantity)
			var nf *domain.NotFoundError
			var v *domain.ValidationError
			switch {
			case tt.wantNotFound && !errors.As(err, &nf):
				t.Errorf("err = %v, want NotFoundError", err)
			case tt.wantField != "" && (!errors.As(err, &v) || v.Field != tt.wantField):
				t.Errorf("err = %v, want a ValidationError on %s", err, tt.wantField)
			}
			if repo.writes != 0 {
				t.Error("the stock was written")
			}
		})
	}
}

func TestProductSetArchived(t *testing.T) {
	repo := newMockProductRepository(existing)
	s := newTestProductService(repo)
	ctx := context.Background()

	p, err := s.SetArchived(ctx, existing.ID, true)
	if err != nil || !p.Archived() {
		t.Fatalf("archiving = %+v, %v", p, err)
	}
	archivedAt := p.ArchivedAt
	again, err := s.SetArchived(ctx, existing.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if repo.writes != 1 || !again.ArchivedAt.Equal(archivedAt) {
		t.Errorf("writes = %d, archived at %v then %v: archiving twice must change nothing",
			repo.writes, archivedAt, again.ArchivedAt)
	}
	if listed, _ := s.List(ctx); len(listed) != 0 {
		t.Errorf("an archived product is listed: %v", listed)
	}
	if n, err := s.CountArchived(ctx); n != 1 || err != nil {
		t.Errorf("CountArchived = %d, %v", n, err)
	}

	if p, err = s.SetArchived(ctx, existing.ID, false); err != nil || p.Archived() {
		t.Fatalf("restoring = %+v, %v", p, err)
	}
	if listed, _ := s.List(ctx); len(listed) != 1 {
		t.Errorf("a restored product is not listed: %v", listed)
	}

	var nf *domain.NotFoundError
	if _, err := s.SetArchived(ctx, uuid.New().String(), true); !errors.As(err, &nf) {
		t.Errorf("missing product: err = %v, want NotFoundError", err)
	}
	repo.writeErr = errors.New("database timeout")
	if _, err := s.SetArchived(ctx, existing.ID, true); !errors.Is(err, repo.writeErr) {
		t.Errorf("failed write: err = %v", err)
	}
}

func TestProductDelete(t *testing.T) {
	repo := newMockProductRepository(existing)
	s := newTestProductService(repo)

	if err := s.Delete(context.Background(), existing.ID); err != nil {
		t.Fatal(err)
	}
	var nf *domain.NotFoundError
	if err := s.Delete(context.Background(), existing.ID); !errors.As(err, &nf) {
		t.Errorf("deleting twice: err = %v, want NotFoundError", err)
	}
}
