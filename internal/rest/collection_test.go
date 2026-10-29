package rest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockCollectionService struct {
	collections []domain.Collection
	listErr     error
	err         error // returned by the writes when set
	deleted     []string
}

func newMockCollectionService() *mockCollectionService { return &mockCollectionService{} }

func (m *mockCollectionService) List(context.Context) ([]domain.Collection, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return slices.Clone(m.collections), nil
}

func (m *mockCollectionService) GetByID(_ context.Context, id string) (domain.Collection, error) {
	for _, c := range m.collections {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Collection{}, domain.NewNotFoundError("collection not found")
}

// check follows the real service's rules on names.
func (m *mockCollectionService) check(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.NewValidationError("name", "Falta o nome da coleção")
	}
	if slices.ContainsFunc(m.collections, func(c domain.Collection) bool { return c.ID != id && strings.EqualFold(c.Name, name) }) {
		return domain.NewValidationError("name", "Já existe uma coleção com esse nome")
	}
	return m.err
}

func (m *mockCollectionService) Create(_ context.Context, name string) (domain.Collection, error) {
	if err := m.check("", name); err != nil {
		return domain.Collection{}, err
	}
	c := domain.Collection{ID: "c" + string(rune('0'+len(m.collections))), Name: strings.TrimSpace(name)}
	m.collections = append(m.collections, c)
	return c, nil
}

func (m *mockCollectionService) Rename(ctx context.Context, id, name string) (domain.Collection, error) {
	if err := m.check(id, name); err != nil {
		return domain.Collection{}, err
	}
	for i := range m.collections {
		if m.collections[i].ID == id {
			m.collections[i].Name = strings.TrimSpace(name)
			return m.collections[i], nil
		}
	}
	return domain.Collection{}, domain.NewNotFoundError("collection not found")
}

func (m *mockCollectionService) Delete(_ context.Context, id string) error {
	if m.err != nil {
		return m.err
	}
	m.deleted = append(m.deleted, id)
	m.collections = slices.DeleteFunc(m.collections, func(c domain.Collection) bool { return c.ID == id })
	return nil
}

func verao() domain.Collection { return domain.Collection{ID: "verao", Name: "Verão 2026", Pieces: 3} }

func TestNewCollectionRejectsMissingDependencies(t *testing.T) {
	if _, err := NewCollection(CollectionInput{}); err == nil {
		t.Error("empty input was accepted")
	}
}

func TestCollectionScreensRequireLogin(t *testing.T) {
	app := newTestApp(t, nil)
	for _, path := range []string{"/admin/collections", "/admin/collections/verao", "/admin/collections/verao/delete"} {
		if res, _ := app.get(t, path, nil); res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login") {
			t.Errorf("%s: status %d, location %q, want the login page", path, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

func TestNavigationMarksTheSection(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	for path, current := range map[string]string{
		"/admin":              `<a href="/admin" aria-current="page">Peças</a>`,
		"/admin/products/new": `<a href="/admin" aria-current="page">Peças</a>`,
		"/admin/collections":  `<a href="/admin/collections" aria-current="page">Coleções</a>`,
	} {
		_, body := app.get(t, path, nil)
		if !strings.Contains(body, current) || strings.Count(body, `aria-current="page"`) != 1 {
			t.Errorf("%s: want only %s marked", path, current)
		}
	}
}

func TestCollectionList(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	_, body := app.get(t, "/admin/collections", nil)
	if !strings.Contains(body, "Nenhuma coleção cadastrada ainda") || !strings.Contains(body, `name="name"`) {
		t.Error("empty list: want the message and the form")
	}

	empty := domain.Collection{ID: "inverno", Name: "Inverno 2026"}
	one := domain.Collection{ID: "outono", Name: "Outono", Pieces: 1}
	app.colls.collections = []domain.Collection{verao(), empty, one}
	_, body = app.get(t, "/admin/collections", nil)
	for _, want := range []string{
		`href="/admin/collections/verao"`, "Verão 2026", "3 peças", "Inverno 2026", "Nenhuma peça ainda", "1 peça<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %s", want)
		}
	}

	app.colls.listErr = errors.New("database down")
	if res, _ := app.get(t, "/admin/collections", nil); res.StatusCode != http.StatusInternalServerError {
		t.Errorf("list failing: status %d, want 500", res.StatusCode)
	}
}

func TestCollectionCreate(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.colls.collections = []domain.Collection{verao()}

	res, _ := app.post(t, "/admin/collections", url.Values{"name": {" Inverno 2026 "}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/collections" {
		t.Fatalf("create: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if len(app.colls.collections) != 2 || app.colls.collections[1].Name != "Inverno 2026" {
		t.Errorf("collections %+v", app.colls.collections)
	}
	if _, body := app.get(t, "/admin/collections", nil); !strings.Contains(body, "A coleção “Inverno 2026” foi cadastrada") {
		t.Error("no confirmation after creating")
	}

	// Refused: the list again, with what was typed and why.
	res, body := app.post(t, "/admin/collections", url.Values{"name": {"verão 2026"}})
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Já existe uma coleção com esse nome") ||
		!strings.Contains(body, `value="verão 2026"`) || !strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, "Inverno 2026") {
		t.Errorf("taken name: status %d", res.StatusCode)
	}

	app.colls.err = errors.New("database down")
	if res, body := app.post(t, "/admin/collections", url.Values{"name": {"Outono"}}); res.StatusCode != http.StatusInternalServerError || strings.Contains(body, "database") {
		t.Errorf("failing: status %d, want 500 without details", res.StatusCode)
	}
}

func TestCollectionRename(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.colls.collections = []domain.Collection{verao(), {ID: "inverno", Name: "Inverno 2026"}}

	_, body := app.get(t, "/admin/collections/verao", nil)
	for _, want := range []string{`value="Verão 2026"`, "3 peças nesta coleção", `href="/admin/collections/verao/delete"`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page lacks %s", want)
		}
	}

	res, _ := app.post(t, "/admin/collections/verao", url.Values{"name": {"Verão 2027"}})
	if res.StatusCode != http.StatusSeeOther || app.colls.collections[0].Name != "Verão 2027" {
		t.Fatalf("rename: status %d, collections %+v", res.StatusCode, app.colls.collections)
	}
	if _, body := app.get(t, "/admin/collections", nil); !strings.Contains(body, "A coleção agora se chama “Verão 2027”") {
		t.Error("no confirmation after renaming")
	}

	res, body = app.post(t, "/admin/collections/verao", url.Values{"name": {""}})
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Falta o nome da coleção") {
		t.Errorf("blank name: status %d", res.StatusCode)
	}

	for _, path := range []string{"/admin/collections/gone", "/admin/collections/gone/delete"} {
		if res, body := app.get(t, path, nil); res.StatusCode != http.StatusNotFound || !strings.Contains(body, "Coleção não encontrada") {
			t.Errorf("%s: status %d, want 404", path, res.StatusCode)
		}
	}
	if res, _ := app.post(t, "/admin/collections/gone", url.Values{"name": {"Outono"}}); res.StatusCode != http.StatusNotFound {
		t.Errorf("rename unknown: status %d, want 404", res.StatusCode)
	}
}

func TestCollectionDelete(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.colls.collections = []domain.Collection{verao()}

	_, body := app.get(t, "/admin/collections/verao/delete", nil)
	if !strings.Contains(body, "As 3 peças dela ficam sem coleção") || !strings.Contains(body, "Nenhuma peça é apagada") {
		t.Error("the confirmation does not say what happens to the pieces")
	}
	if !slices.Equal(app.colls.deleted, nil) {
		t.Fatal("deleted before confirming")
	}

	res, _ := app.post(t, "/admin/collections/verao/delete", url.Values{})
	if res.StatusCode != http.StatusSeeOther || !slices.Equal(app.colls.deleted, []string{"verao"}) {
		t.Fatalf("delete: status %d, deleted %v", res.StatusCode, app.colls.deleted)
	}
	if _, body := app.get(t, "/admin/collections", nil); !strings.Contains(body, "A coleção “Verão 2026” foi apagada") {
		t.Error("no confirmation after deleting")
	}
}

func TestProductFormCollectionSelect(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	// None yet: only "Sem coleção", and where to register one.
	_, body := app.get(t, "/admin/products/new", nil)
	if !strings.Contains(body, `<select id="collection" name="collection_id"`) || !strings.Contains(body, `<option value="">Sem coleção</option>`) ||
		!strings.Contains(body, "Ainda não há coleções") {
		t.Error("new product page without collections")
	}

	app.colls.collections = []domain.Collection{verao(), {ID: "inverno", Name: "Inverno 2026"}}
	p := aurora()
	p.CollectionID, p.CollectionName = "verao", "Verão 2026"
	app.products.add(p)
	_, body = app.get(t, "/admin/products/"+auroraID, nil)
	for _, want := range []string{`<option value="verao" selected>Verão 2026</option>`, `<option value="inverno" >Inverno 2026</option>`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form lacks %s", want)
		}
	}
	// The list says which collection each piece is in.
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, `<span class="meta">Verão 2026 · R$`) {
		t.Error("the list does not show the piece's collection")
	}

	// Without the list, the piece keeps its collection and can be saved.
	app.colls.listErr = errors.New("database down")
	res, body := app.get(t, "/admin/products/"+auroraID, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `<option value="verao" selected>Verão 2026</option>`) ||
		!strings.Contains(body, "Não consegui carregar a lista de coleções") {
		t.Errorf("edit form without the list: status %d", res.StatusCode)
	}
}
