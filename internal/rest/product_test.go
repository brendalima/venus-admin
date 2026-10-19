package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

const auroraID = "8a0e2f9e-4a8b-4c55-9d4c-6f1b7b0f2a11"

// mockProductService stores products in memory. Create and Update run the
// domain's validation, so the handlers see the same joined errors the real
// service returns.
type mockProductService struct {
	products map[string]domain.Product // by ID
	listErr  error
	countErr error
	getErr   error
	writeErr error // returned by Create, Update, SetSoldOut, SetArchived and Delete
	created  []domain.Product
	updated  []domain.Product
	photoErr error               // returned by Create and Update when a photo edit is sent
	edits    []*domain.PhotoEdit // every photo edit received, nil included
}

func newMockProductService() *mockProductService {
	return &mockProductService{products: map[string]domain.Product{}}
}

func (m *mockProductService) add(p domain.Product) {
	m.products[p.ID] = p
}

func (m *mockProductService) List(context.Context) ([]domain.Product, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.list(false), nil
}

func (m *mockProductService) ListArchived(context.Context) ([]domain.Product, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.list(true), nil
}

func (m *mockProductService) CountArchived(context.Context) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return len(m.list(true)), nil
}

// list follows the real service: the collection and the archived pieces
// are never listed together.
func (m *mockProductService) list(archived bool) []domain.Product {
	ps := []domain.Product{}
	for _, p := range m.products {
		if p.Archived() == archived {
			ps = append(ps, p)
		}
	}
	slices.SortFunc(ps, func(a, b domain.Product) int { return strings.Compare(a.Name, b.Name) })
	return ps
}

func (m *mockProductService) GetByID(_ context.Context, id string) (domain.Product, error) {
	if m.getErr != nil {
		return domain.Product{}, m.getErr
	}
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	return p, nil
}

func (m *mockProductService) Create(_ context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error) {
	m.edits = append(m.edits, photos)
	p = p.Normalized()
	p.Slug = domain.Slugify(p.Name)
	images, err := m.applyPhotos(nil, photos, p.Validate())
	if err != nil {
		return domain.Product{}, err
	}
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	p.ID = fmt.Sprintf("new-%d", len(m.created)+1)
	p.Images = images
	m.created = append(m.created, p)
	m.products[p.ID] = p
	return p, nil
}

func (m *mockProductService) Update(_ context.Context, p domain.Product, photos *domain.PhotoEdit) (domain.Product, error) {
	m.edits = append(m.edits, photos)
	p = p.Normalized()
	if p.Slug = domain.Slugify(p.Slug); p.Slug == "" {
		p.Slug = domain.Slugify(p.Name)
	}
	current, ok := m.products[p.ID]
	if !ok {
		return domain.Product{}, fmt.Errorf("couldn't load product: %w", domain.NewNotFoundError("product not found"))
	}
	images, err := m.applyPhotos(current.Images, photos, p.Validate())
	if err != nil {
		return domain.Product{}, err
	}
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	p.Images = images
	p.ArchivedAt = current.ArchivedAt
	m.updated = append(m.updated, p)
	m.products[p.ID] = p
	return p, nil
}

// applyPhotos follows the real service's rules closely enough for the
// handlers: every problem at once, a file reading "not a photo" refused by
// index, and a photo the screen did not know about making the edit stale.
func (m *mockProductService) applyPhotos(current []domain.Image, edit *domain.PhotoEdit, fieldErr error) ([]domain.Image, error) {
	if edit == nil {
		return current, fieldErr
	}
	if m.photoErr != nil {
		return nil, m.photoErr
	}
	e := *edit
	if e.AppendNew {
		e.Order = nil
		for _, img := range current {
			e.Order = append(e.Order, domain.PhotoSlot{ImageID: img.ID})
		}
		for i := range e.New {
			e.Order = append(e.Order, domain.PhotoSlot{NewIndex: i})
		}
	}
	if err := e.Validate(); err != nil {
		return nil, errors.Join(fieldErr, err)
	}
	byID := map[string]domain.Image{}
	for _, img := range current {
		byID[img.ID] = img
	}
	errs := []error{fieldErr}
	for i, src := range e.New {
		data, err := src.Bytes()
		if err != nil {
			return nil, err
		}
		if string(data) == notAPhoto {
			errs = append(errs, domain.NewValidationError(domain.PhotoErrorField(i), "Esse arquivo não é uma foto em JPEG, PNG ou WebP."))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	images := []domain.Image{}
	for _, slot := range e.Order {
		if slot.ImageID == "" {
			id := fmt.Sprintf("added-%d", slot.NewIndex)
			images = append(images, domain.Image{ID: id, Key600: "produtos/" + id + "-600.jpg"})
			continue
		}
		img, ok := byID[slot.ImageID]
		if !ok {
			return nil, domain.NewStaleError("photos changed since they were loaded")
		}
		images = append(images, img)
	}
	return images, nil
}

func (m *mockProductService) SetSoldOut(_ context.Context, id string, soldOut bool) (domain.Product, error) {
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	p.SoldOut = soldOut
	m.products[id] = p
	return p, nil
}

func (m *mockProductService) SetArchived(_ context.Context, id string, archived bool) (domain.Product, error) {
	if m.writeErr != nil {
		return domain.Product{}, m.writeErr
	}
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.NewNotFoundError("product not found")
	}
	p.ArchivedAt = time.Time{}
	if archived {
		p.ArchivedAt = time.Date(2026, 10, 19, 1, 30, 0, 0, time.UTC) // still the 18th in Brasília
	}
	m.products[id] = p
	return p, nil
}

func (m *mockProductService) Delete(_ context.Context, id string) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	if _, ok := m.products[id]; !ok {
		return domain.NewNotFoundError("product not found")
	}
	delete(m.products, id)
	return nil
}

func aurora() domain.Product {
	return domain.Product{
		ID: auroraID, Slug: "biquini-aurora", Name: "Biquíni Aurora", PriceCents: 18990,
		Sizes: []string{"P", "M"}, Description: "Triângulo verde",
		Details: []string{"Verde esmeralda", "Forro duplo"},
	}
}

func validForm() url.Values {
	return url.Values{
		"name":        {"Biquíni Aurora"},
		"price":       {"189,90"},
		"sizes":       {"P", "M"},
		"description": {"Triângulo verde"},
		"details":     {"Verde esmeralda\r\n\r\nForro duplo\r\n"},
	}
}

func (a *testApp) postHTMX(t *testing.T, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return a.do(t, req)
}

func TestNewProductRejectsMissingDependencies(t *testing.T) {
	if _, err := NewProduct(ProductInput{}); err == nil {
		t.Error("empty input was accepted")
	}
}

func TestProductScreensRequireLogin(t *testing.T) {
	app := newTestApp(t, nil)

	for _, path := range []string{"/admin", "/admin/products/new", "/admin/products/" + auroraID, "/admin/arquivadas"} {
		if res, _ := app.get(t, path, nil); res.Header.Get("Location") != "/login" {
			t.Errorf("anonymous GET %s: status %d, location %q", path, res.StatusCode, res.Header.Get("Location"))
		}
	}

	res, _ := app.post(t, "/admin/products", validForm())
	if res.Header.Get("Location") != "/login" || len(app.products.created) != 0 {
		t.Errorf("anonymous create: status %d, created %d", res.StatusCode, len(app.products.created))
	}
}

func TestProductList(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	_, body := app.get(t, "/admin", nil)
	if !strings.Contains(body, "Nenhuma peça cadastrada ainda") {
		t.Error("an empty catalogue does not say so")
	}

	soldOut := aurora()
	soldOut.ID, soldOut.Name, soldOut.SoldOut = "other", "Maiô Órion", true
	app.products.add(aurora())
	app.products.add(soldOut)

	res, body := app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	for _, want := range []string{"Biquíni Aurora", "R$ 189,90", "P M", "Maiô Órion", "Esgotada", "Voltou ao estoque", "Marcar como esgotada"} {
		if !strings.Contains(body, want) {
			t.Errorf("list does not show %q", want)
		}
	}

	app.products.listErr = errors.New("database timeout")
	res, body = app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(body, "Não consegui carregar as peças") {
		t.Errorf("list failure: status %d", res.StatusCode)
	}
	if strings.Contains(body, "database timeout") {
		t.Error("an internal error leaked into the page")
	}
}

func TestProductCreate(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	if res, body := app.get(t, "/admin/products/new", nil); res.StatusCode != http.StatusOK || !strings.Contains(body, `name="price"`) {
		t.Fatalf("new product page: status %d", res.StatusCode)
	}

	res, _ := app.post(t, "/admin/products", validForm())
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("create: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}

	if len(app.products.created) != 1 {
		t.Fatalf("created %d products, want 1", len(app.products.created))
	}
	p := app.products.created[0]
	if p.PriceCents != 18990 {
		t.Errorf("price = %d cents, want 18990", p.PriceCents)
	}
	if !slices.Equal(p.Sizes, []string{"P", "M"}) || !slices.Equal(p.Details, []string{"Verde esmeralda", "Forro duplo"}) {
		t.Errorf("sizes %v, details %v", p.Sizes, p.Details)
	}

	// The outcome is shown once, on the page the browser lands on.
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "“Biquíni Aurora” foi cadastrada.") {
		t.Error("no confirmation after creating")
	}
	if _, body := app.get(t, "/admin", nil); strings.Contains(body, "foi cadastrada") {
		t.Error("the confirmation was shown twice")
	}
}

func TestProductCreateRejected(t *testing.T) {
	tests := []struct {
		name       string
		edit       func(url.Values)
		writeErr   error
		wantStatus int
		wantText   []string
		wantValue  string // a typed value the form must still hold
	}{
		{
			name:       "every problem at once",
			edit:       func(f url.Values) { f.Set("name", " "); f.Del("sizes") },
			wantStatus: http.StatusBadRequest,
			wantText:   []string{"Nada foi salvo. Confira o que está marcado", "Falta o nome da peça", "Escolha ao menos um tamanho"},
			wantValue:  "189,90",
		},
		{
			name:       "unreadable price",
			edit:       func(f url.Values) { f.Set("price", "18x") },
			wantStatus: http.StatusBadRequest,
			wantText:   []string{"Escreva o preço só com números, como 189,90"},
			wantValue:  "18x",
		},
		{
			// Never saved as "no price": the service is not even called,
			// yet the other fields are still checked.
			name:       "unreadable price and no name",
			edit:       func(f url.Values) { f.Set("price", "18x"); f.Set("name", "") },
			wantStatus: http.StatusBadRequest,
			wantText:   []string{`id="price-error"`, "Falta o nome da peça"},
			wantValue:  "18x",
		},
		{
			// The slug comes from the name on creation, so its error
			// shows next to the name: there is no address field yet.
			name:       "name with no letters",
			edit:       func(f url.Values) { f.Set("name", "!!!") },
			wantStatus: http.StatusBadRequest,
			wantText:   []string{`id="name-error"`, "Não consegui gerar o endereço"},
			wantValue:  "!!!",
		},
		{
			name:       "service down",
			writeErr:   errors.New("database timeout"),
			wantStatus: http.StatusInternalServerError,
			wantText:   []string{"Nada foi salvo: tente de novo"},
			wantValue:  "Biquíni Aurora",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.login(t)
			app.products.writeErr = tt.writeErr

			form := validForm()
			if tt.edit != nil {
				tt.edit(form)
			}
			res, body := app.post(t, "/admin/products", form)

			if res.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(body, want) {
					t.Errorf("page does not contain %q", want)
				}
			}
			if !strings.Contains(body, tt.wantValue) {
				t.Errorf("the form lost %q", tt.wantValue)
			}
			if strings.Contains(body, "database timeout") {
				t.Error("an internal error leaked into the page")
			}
			if len(app.products.created) != 0 {
				t.Error("a rejected product was saved")
			}
		})
	}
}

// Pieces are catalogued before they are priced.
func TestProductWithoutPrice(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	form := validForm()
	form.Set("price", "")
	res, _ := app.post(t, "/admin/products", form)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create without a price: status %d, want 303", res.StatusCode)
	}
	p := app.products.created[0]
	if p.PriceCents != 0 {
		t.Errorf("price = %d cents, want 0", p.PriceCents)
	}

	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "Sem preço") || strings.Contains(body, "R$ 0,00") {
		t.Error("the list does not say the price is missing")
	}
	// An empty field, not "0,00", which someone could save as a price.
	if _, body := app.get(t, "/admin/products/"+p.ID, nil); strings.Contains(body, `value="0,00"`) || !strings.Contains(body, `id="price" name="price" type="text" inputmode="decimal" value=""`) {
		t.Error("the edit form does not show an empty price")
	}
}

func TestProductEdit(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())

	res, body := app.get(t, "/admin/products/"+auroraID, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	for _, want := range []string{`value="Biquíni Aurora"`, `value="189,90"`, "Verde esmeralda\nForro duplo", `value="biquini-aurora"`, "Tirar da coleção"} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form does not contain %q", want)
		}
	}
	if !strings.Contains(body, `value="P" checked`) || strings.Contains(body, `value="G" checked`) {
		t.Error("the sizes are not ticked as stored")
	}

	res, body = app.get(t, "/admin/products/does-not-exist", nil)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "Peça não encontrada") {
		t.Errorf("unknown product: status %d", res.StatusCode)
	}
}

func TestProductUpdate(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())

	form := validForm()
	form.Set("price", "199")
	form.Set("slug", "biquini-aurora")
	form.Set("sold_out", "on")
	res, _ := app.post(t, "/admin/products/"+auroraID, form)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("update: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}

	p := app.products.products[auroraID]
	if p.PriceCents != 19900 || !p.SoldOut || p.Slug != "biquini-aurora" {
		t.Errorf("stored %+v", p)
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "As alterações em “Biquíni Aurora” foram salvas.") {
		t.Error("no confirmation after saving")
	}

	res, body := app.post(t, "/admin/products/does-not-exist", form)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "Peça não encontrada") {
		t.Errorf("unknown product: status %d", res.StatusCode)
	}
}

func TestProductUpdateSlugTaken(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())
	app.products.writeErr = domain.NewValidationError("slug", "Já existe outra peça com esse endereço. Escolha outro.")

	form := validForm()
	form.Set("slug", "biquini-eclipse")
	res, body := app.post(t, "/admin/products/"+auroraID, form)

	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
	// The field sits under "advanced options", which must open to show it.
	if !strings.Contains(body, "<details open>") || !strings.Contains(body, "Já existe outra peça com esse endereço") {
		t.Error("the slug error is hidden or missing")
	}
	if !strings.Contains(body, `value="biquini-eclipse"`) {
		t.Error("the typed slug was lost")
	}
}

func TestProductSetSoldOut(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())
	path := "/admin/products/" + auroraID + "/sold-out"

	// Through HTMX: the row alone, to swap in place.
	res, body := app.postHTMX(t, path, url.Values{"sold_out": {"true"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "<li") || strings.Contains(body, "<html") {
		t.Errorf("want only the row, got:\n%s", body)
	}
	if !strings.Contains(body, "Voltou ao estoque") || !app.products.products[auroraID].SoldOut {
		t.Error("the product was not marked sold out")
	}

	// Without JavaScript: back to the list.
	res, _ = app.post(t, path, url.Values{"sold_out": {"false"}})
	if res.StatusCode != http.StatusSeeOther || app.products.products[auroraID].SoldOut {
		t.Errorf("plain form: status %d, sold out %v", res.StatusCode, app.products.products[auroraID].SoldOut)
	}

	if res, _ := app.post(t, path, url.Values{"sold_out": {"talvez"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unreadable value: status %d, want 400", res.StatusCode)
	}

	// A product gone in the meantime: HTMX reloads the list, which says why.
	res, _ = app.postHTMX(t, "/admin/products/gone/sold-out", url.Values{"sold_out": {"true"}})
	if res.Header.Get("HX-Redirect") != "/admin" {
		t.Errorf("missing product: HX-Redirect %q, want /admin", res.Header.Get("HX-Redirect"))
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "Essa peça não existe mais") {
		t.Error("the list does not explain what happened")
	}
}

func TestProductDelete(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())
	path := "/admin/products/" + auroraID + "/delete"

	res, body := app.get(t, path, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Apagar “Biquíni Aurora” de vez?") {
		t.Fatalf("confirmation page: status %d", res.StatusCode)
	}
	if _, ok := app.products.products[auroraID]; !ok {
		t.Fatal("showing the confirmation deleted the product")
	}

	app.products.writeErr = errors.New("database timeout")
	if res, _ := app.post(t, path, nil); res.StatusCode != http.StatusInternalServerError {
		t.Errorf("failed delete: status %d, want 500", res.StatusCode)
	}
	app.products.writeErr = nil

	res, _ = app.post(t, path, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/arquivadas" {
		t.Fatalf("delete: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if _, ok := app.products.products[auroraID]; ok {
		t.Error("the product is still there")
	}
	if _, body := app.get(t, "/admin/arquivadas", nil); !strings.Contains(body, "“Biquíni Aurora” foi apagada de vez.") {
		t.Error("no confirmation after deleting")
	}

	if res, _ := app.post(t, path, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("deleting again: status %d, want 404", res.StatusCode)
	}
}

func TestProductArchive(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())
	path := "/admin/products/" + auroraID + "/archive"

	// The edit screen offers archiving, not deleting.
	_, body := app.get(t, "/admin/products/"+auroraID, nil)
	if !strings.Contains(body, `href="`+path+`"`) || strings.Contains(body, "/delete") {
		t.Errorf("edit screen: want a link to archive and none to delete")
	}

	res, body := app.get(t, path, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Tirar “Biquíni Aurora” da coleção?") {
		t.Fatalf("confirmation page: status %d", res.StatusCode)
	}
	if app.products.products[auroraID].Archived() {
		t.Fatal("showing the confirmation archived the product")
	}

	res, _ = app.post(t, path, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("archive: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if !app.products.products[auroraID].Archived() {
		t.Fatal("the product was not archived")
	}

	_, body = app.get(t, "/admin", nil)
	if !strings.Contains(body, "saiu da coleção") || !strings.Contains(body, "Nenhuma peça cadastrada ainda") {
		t.Error("the list still shows the archived product, or does not say it left")
	}
	if !strings.Contains(body, "Peças arquivadas (1)") {
		t.Error("the list does not lead to the archived pieces with their count")
	}

	res, body = app.get(t, "/admin/arquivadas", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("archived list: status %d", res.StatusCode)
	}
	for _, want := range []string{"Biquíni Aurora", "Arquivada em 18/10/2026", "Voltar para a coleção", "/admin/products/" + auroraID + "/delete"} {
		if !strings.Contains(body, want) {
			t.Errorf("archived list does not show %q", want)
		}
	}

	// An archived piece can still be edited, and is offered back.
	_, body = app.get(t, "/admin/products/"+auroraID, nil)
	if !strings.Contains(body, "Esta peça está arquivada") || strings.Contains(body, `href="`+path+`"`) {
		t.Error("the edit screen of an archived piece does not say so")
	}
	res, _ = app.post(t, "/admin/products/"+auroraID, validForm())
	if res.Header.Get("Location") != "/admin/arquivadas" || !app.products.products[auroraID].Archived() {
		t.Errorf("saving an archived piece: location %q, archived %v",
			res.Header.Get("Location"), app.products.products[auroraID].Archived())
	}
}

func TestProductUnarchive(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	archived := aurora()
	archived.ArchivedAt = time.Now()
	app.products.add(archived)
	path := "/admin/products/" + auroraID + "/unarchive"

	// Through HTMX, from the archived list: a row saying so, in place.
	res, body := app.postHTMX(t, path, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "<li") || !strings.Contains(body, "voltou para a coleção") {
		t.Errorf("want only the row saying it is back, got:\n%s", body)
	}
	if app.products.products[auroraID].Archived() {
		t.Fatal("the product is still archived")
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "Biquíni Aurora") || !strings.Contains(body, "Peças arquivadas (0)") {
		t.Error("the restored piece is not back in the list")
	}
	if _, body := app.get(t, "/admin/arquivadas", nil); !strings.Contains(body, "Nenhuma peça arquivada") {
		t.Error("an empty archive does not say so")
	}

	// Without JavaScript, and asked twice: back to the list, no harm done.
	res, _ = app.post(t, path, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Errorf("plain form: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}

	res, _ = app.postHTMX(t, "/admin/products/gone/unarchive", nil)
	if res.Header.Get("HX-Redirect") != "/admin" {
		t.Errorf("missing product: HX-Redirect %q, want /admin", res.Header.Get("HX-Redirect"))
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "Essa peça não existe mais") {
		t.Error("the list does not explain what happened")
	}
}

func TestProductArchiveFailures(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())

	app.products.writeErr = errors.New("database timeout")
	res, _ := app.post(t, "/admin/products/"+auroraID+"/archive", nil)
	if res.Header.Get("Location") != "/admin" || app.products.products[auroraID].Archived() {
		t.Errorf("failed archive: location %q", res.Header.Get("Location"))
	}
	_, body := app.get(t, "/admin", nil)
	if !strings.Contains(body, "Não consegui salvar agora") || strings.Contains(body, "database timeout") {
		t.Error("a failed archive is not explained, or leaks the error")
	}
	app.products.writeErr = nil

	// The count is only a hint: the list shows without it.
	app.products.countErr = errors.New("database timeout")
	res, body = app.get(t, "/admin", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Peças arquivadas</a>") {
		t.Errorf("count failure: status %d, want the link without a number", res.StatusCode)
	}

	app.products.listErr = errors.New("database timeout")
	res, body = app.get(t, "/admin/arquivadas", nil)
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(body, "Não consegui carregar as peças arquivadas") {
		t.Errorf("archived list failure: status %d", res.StatusCode)
	}

	if res, _ := app.get(t, "/admin/products/gone/archive", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("confirming a missing product: status %d, want 404", res.StatusCode)
	}
}

func TestStaticFiles(t *testing.T) {
	app := newTestApp(t, nil)

	res, body := app.get(t, "/static/htmx-2.0.4.min.js", nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(body, "var htmx=") {
		t.Fatalf("htmx: status %d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
		t.Errorf("Content-Type = %q", res.Header.Get("Content-Type"))
	}
	if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("Cache-Control = %q", res.Header.Get("Cache-Control"))
	}

	for _, path := range []string{"/static/", "/static/missing.js", "/static/../templates/base.html"} {
		res, _ := app.get(t, path, nil)
		if res.StatusCode == http.StatusOK || strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("GET %s: status %d, Cache-Control %q", path, res.StatusCode, res.Header.Get("Cache-Control"))
		}
	}
}

func TestFieldErrors(t *testing.T) {
	err := fmt.Errorf("couldn't save: %w", errors.Join(
		domain.NewValidationError("name", "Falta o nome da peça"),
		domain.NewValidationError("sizes", "Escolha ao menos um tamanho"),
		domain.NewValidationError("sizes", "Tamanho desconhecido: XG"),
	))
	got := fieldErrors(err)
	want := map[string]string{"name": "Falta o nome da peça", "sizes": "Escolha ao menos um tamanho"}
	if len(got) != len(want) || got["name"] != want["name"] || got["sizes"] != want["sizes"] {
		t.Errorf("fieldErrors = %v, want %v", got, want)
	}
	if got := fieldErrors(errors.New("timeout")); len(got) != 0 {
		t.Errorf("an infrastructure error produced field errors: %v", got)
	}
}
