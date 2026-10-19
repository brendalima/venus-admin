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
}

func newMockProductRepository(ps ...domain.Product) *mockProductRepository {
	m := &mockProductRepository{products: map[string]domain.Product{}}
	for _, p := range ps {
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
	p.Images = m.products[p.ID].Images // a plain update leaves them alone
	m.products[p.ID] = p
	return p, nil
}

// UpdateWithImages trusts the service's own staleness check, which is
// what these tests exercise; the real one is covered by the repository's
// integration tests.
func (m *mockProductRepository) UpdateWithImages(_ context.Context, p domain.Product, _, removed []string) (domain.Product, []domain.Image, error) {
	if m.writeErr != nil {
		return domain.Product{}, nil, m.writeErr
	}
	m.writes++
	var gone []domain.Image
	for _, img := range m.products[p.ID].Images {
		if slices.Contains(removed, img.ID) {
			gone = append(gone, img)
		}
	}
	m.products[p.ID] = p
	return p, gone, nil
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

// existing is a stored product the update tests start from.
var existing = domain.Product{
	ID: "8a0e2f9e-4a8b-4c55-9d4c-6f1b7b0f2a11", Slug: "biquini-aurora", Name: "Biquíni Aurora",
	PriceCents: 18900, Sizes: []string{"P"}, Position: 7,
}

func TestNewProduct(t *testing.T) {
	full := ProductInput{Repo: newMockProductRepository(), Store: newMockImageStore(), Processor: &mockProcessor{}}
	for name, in := range map[string]ProductInput{
		"repository": {Store: full.Store, Processor: full.Processor},
		"store":      {Repo: full.Repo, Processor: full.Processor},
		"processor":  {Repo: full.Repo, Store: full.Store},
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

	p, err := s.Create(context.Background(), validInput(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func TestProductCreateFailures(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(*domain.Product)
		writeErr  error
		wantField string // validation error expected on this field
	}{
		{name: "invalid", edit: func(p *domain.Product) { p.Name, p.PriceCents = "", 0 }, wantField: "name"},
		{name: "name taken", writeErr: domain.NewConflictError("slug already in use"), wantField: "name"},
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

			_, err := newTestProductService(repo).Create(context.Background(), in, nil)

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
		wantField string
		notFound  bool
	}{
		{name: "missing product", id: uuid.New().String(), notFound: true},
		{name: "slug taken", id: existing.ID, writeErr: domain.NewConflictError("slug already in use"), wantField: "slug"},
		{name: "database down", id: existing.ID, writeErr: errors.New("timeout")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockProductRepository(existing)
			repo.writeErr = tt.writeErr
			in := validInput()
			in.ID = tt.id

			_, err := newTestProductService(repo).Update(context.Background(), in, nil)

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
