package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockCollectionRepository struct {
	collections []domain.Collection
	err         error // returned by every method when set
}

func newMockCollectionRepository(cs ...domain.Collection) *mockCollectionRepository {
	return &mockCollectionRepository{collections: cs}
}

func (m *mockCollectionRepository) List(context.Context) ([]domain.Collection, error) {
	return slices.Clone(m.collections), m.err
}

func (m *mockCollectionRepository) GetByID(_ context.Context, id string) (domain.Collection, error) {
	if m.err != nil {
		return domain.Collection{}, m.err
	}
	for _, c := range m.collections {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Collection{}, domain.NewNotFoundError("collection not found")
}

// taken follows the unique index: names clash in any case.
func (m *mockCollectionRepository) taken(id, name string) bool {
	return slices.ContainsFunc(m.collections, func(c domain.Collection) bool {
		return c.ID != id && strings.EqualFold(c.Name, name)
	})
}

func (m *mockCollectionRepository) Create(_ context.Context, c domain.Collection) (domain.Collection, error) {
	if m.err != nil {
		return domain.Collection{}, m.err
	}
	if m.taken(c.ID, c.Name) {
		return domain.Collection{}, domain.NewConflictError(domain.ConflictCollection)
	}
	m.collections = append(m.collections, c)
	return c, nil
}

func (m *mockCollectionRepository) Rename(ctx context.Context, id, name string) (domain.Collection, error) {
	if m.err != nil {
		return domain.Collection{}, m.err
	}
	if m.taken(id, name) {
		return domain.Collection{}, domain.NewConflictError(domain.ConflictCollection)
	}
	for i := range m.collections {
		if m.collections[i].ID == id {
			m.collections[i].Name = name
			return m.collections[i], nil
		}
	}
	return domain.Collection{}, domain.NewNotFoundError("collection not found")
}

func (m *mockCollectionRepository) Delete(_ context.Context, id string) error {
	if m.err != nil {
		return m.err
	}
	n := len(m.collections)
	m.collections = slices.DeleteFunc(m.collections, func(c domain.Collection) bool { return c.ID == id })
	if len(m.collections) == n {
		return domain.NewNotFoundError("collection not found")
	}
	return nil
}

func TestNewCollection(t *testing.T) {
	if _, err := NewCollection(nil); err == nil {
		t.Error("missing repository was accepted")
	}
}

func TestCollectionCreate(t *testing.T) {
	ctx := context.Background()
	repo := newMockCollectionRepository(domain.Collection{ID: "verao", Name: "Verão 2026"})
	s := MustNewCollection(repo)

	c, err := s.Create(ctx, "  Inverno   2026 ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Inverno 2026" || uuid.Validate(c.ID) != nil {
		t.Errorf("created %+v, want the name tidied and a generated UUID", c)
	}

	for name, in := range map[string]string{"blank": "  ", "taken in other case": "verão 2026"} {
		_, err := s.Create(ctx, in)
		if !slices.Equal(fieldsOf(err), []string{"name"}) {
			t.Errorf("%s: err = %v, want a validation error on name", name, err)
		}
	}
	if len(repo.collections) != 2 {
		t.Errorf("%d collections stored, want 2", len(repo.collections))
	}

	repo.err = errors.New("database down")
	var invalid *domain.ValidationError
	if _, err := s.Create(ctx, "Outono"); err == nil || errors.As(err, &invalid) {
		t.Errorf("err = %v, want a plain error", err)
	}
}

func TestCollectionRename(t *testing.T) {
	ctx := context.Background()
	repo := newMockCollectionRepository(
		domain.Collection{ID: "verao", Name: "Verão 2026"}, domain.Collection{ID: "inverno", Name: "Inverno 2026"})
	s := MustNewCollection(repo)

	c, err := s.Rename(ctx, "verao", " Verão  2027 ")
	if err != nil || c.Name != "Verão 2027" {
		t.Fatalf("renamed %+v, %v", c, err)
	}
	// Its own name in another case is not a clash.
	if _, err := s.Rename(ctx, "verao", "VERÃO 2027"); err != nil {
		t.Errorf("renaming to its own name in capitals: %v", err)
	}
	if _, err := s.Rename(ctx, "verao", "inverno 2026"); !slices.Equal(fieldsOf(err), []string{"name"}) {
		t.Errorf("taken name: err = %v, want a validation error on name", err)
	}
	if _, err := s.Rename(ctx, "verao", ""); !slices.Equal(fieldsOf(err), []string{"name"}) {
		t.Errorf("blank name: err = %v, want a validation error on name", err)
	}
	var notFound *domain.NotFoundError
	if _, err := s.Rename(ctx, "gone", "Outono"); !errors.As(err, &notFound) {
		t.Errorf("unknown collection: err = %v, want not found", err)
	}
}
