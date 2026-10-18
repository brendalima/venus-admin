package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// notAPhoto is what the mock's AddPhoto refuses, as the processor would.
const notAPhoto = "not a photo"

func (m *mockProductService) AddPhoto(_ context.Context, productID string, original []byte) (domain.Image, error) {
	if m.photoErr != nil {
		return domain.Image{}, m.photoErr
	}
	p, ok := m.products[productID]
	if !ok {
		return domain.Image{}, domain.NewNotFoundError("product not found")
	}
	if string(original) == notAPhoto {
		return domain.Image{}, domain.NewValidationError("photos", "Esse arquivo não é uma foto em JPEG, PNG ou WebP.")
	}
	id := fmt.Sprintf("img-%d", len(p.Images)+1)
	img := domain.Image{ID: id, ProductID: productID, Key600: "produtos/" + id + "-600.jpg", Key1200: "produtos/" + id + "-1200.jpg"}
	p.Images = append(p.Images, img)
	m.products[productID] = p
	return img, nil
}

func (m *mockProductService) MovePhoto(_ context.Context, productID, imageID string, index int) error {
	if m.photoErr != nil {
		return m.photoErr
	}
	p := m.products[productID]
	for _, img := range p.Images {
		if img.ID == imageID {
			m.moves = append(m.moves, index)
			return nil
		}
	}
	return domain.NewNotFoundError("image not found")
}

func (m *mockProductService) DeletePhoto(_ context.Context, productID, imageID string) error {
	if m.photoErr != nil {
		return m.photoErr
	}
	p := m.products[productID]
	for i, img := range p.Images {
		if img.ID == imageID {
			p.Images = slices.Delete(p.Images, i, i+1)
			m.products[productID] = p
			return nil
		}
	}
	return domain.NewNotFoundError("image not found")
}

func auroraWithPhotos(n int) domain.Product {
	p := aurora()
	for i := range n {
		id := fmt.Sprintf("img-%d", i+1)
		p.Images = append(p.Images, domain.Image{ID: id, ProductID: p.ID, Key600: "produtos/" + id + "-600.jpg"})
	}
	return p
}

// postFiles sends a multipart form with one "photos" part per file.
func (a *testApp) postFiles(t *testing.T, path string, files map[string][]byte) (*http.Response, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		part, err := mw.CreateFormFile("photos", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.do(t, req)
}

func TestEditPageShowsPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(3))

	_, body := app.get(t, "/admin/products/"+auroraID, nil)
	for _, want := range []string{
		`src="` + testImageBaseURL + `/produtos/img-1-600.jpg"`,
		`alt="Foto 1 de Biquíni Aurora"`,
		`>Capa<`,
		`action="/admin/products/` + auroraID + `/photos/img-2/move"`,
		`enctype="multipart/form-data"`,
		`accept="image/jpeg,image/png,image/webp"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page lacks %s", want)
		}
	}
	// The first photo cannot go further up, nor the last further down.
	if strings.Count(body, "↑ Antes") != 2 || strings.Count(body, "↓ Depois") != 2 {
		t.Errorf("found %d “antes” and %d “depois” buttons, want 2 of each",
			strings.Count(body, "↑ Antes"), strings.Count(body, "↓ Depois"))
	}

	app.products.add(auroraWithPhotos(domain.MaxImages))
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); strings.Contains(body, `type="file"`) || !strings.Contains(body, "o máximo") {
		t.Error("a full product still offers to add photos")
	}
}

func TestListShowsCover(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(2))
	bare := aurora()
	bare.ID, bare.Name = "other", "Maiô Brisa"
	app.products.add(bare)

	_, body := app.get(t, "/admin", nil)
	if !strings.Contains(body, `class="thumb" src="`+testImageBaseURL+`/produtos/img-1-600.jpg"`) {
		t.Error("the list does not show the cover")
	}
	if strings.Count(body, "Sem fotos") != 1 {
		t.Error("the product without photos is not marked")
	}
}

func TestCreateGoesToPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	res, _ := app.post(t, "/admin/products", validForm())
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/products/new-1#fotos" {
		t.Fatalf("create: status %d, location %q, want 303 to the photos", res.StatusCode, res.Header.Get("Location"))
	}
	_, body := app.get(t, "/admin/products/new-1", nil)
	if i := strings.Index(body, `id="fotos"`); i < 0 || !strings.Contains(body[i:], "Agora adicione as fotos") {
		t.Error("the photos section does not say what comes next")
	}
}

func TestUploadPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(aurora())

	res, _ := app.postFiles(t, "/admin/products/"+auroraID+"/photos", map[string][]byte{
		"IMG_0001.JPG":  []byte("photo one"),
		"IMG_0002.JPG":  []byte("photo two"),
		"IMG_0003.HEIC": []byte(notAPhoto),
		"IMG_0004.HEIC": []byte(notAPhoto),
		"enorme.png":    bytes.Repeat([]byte("x"), maxPhotoBytes+1),
	})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/products/"+auroraID+"#fotos" {
		t.Fatalf("upload: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if n := len(app.products.products[auroraID].Images); n != 2 {
		t.Errorf("%d photos added, want 2", n)
	}

	_, body := app.get(t, "/admin/products/"+auroraID, nil)
	// The page lands on #fotos, so that is where the outcome must be.
	section := body[strings.Index(body, `id="fotos"`):]
	for _, want := range []string{
		"2 fotos adicionadas.",
		"IMG_0003.HEIC, IMG_0004.HEIC: Esse arquivo não é uma foto em JPEG, PNG ou WebP.",
		"enorme.png: passa de 15 MB.",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the photos section does not say %q", want)
		}
	}
}

func TestUploadPhotosFailures(t *testing.T) {
	tests := []struct {
		name       string
		productID  string
		files      map[string][]byte
		photoErr   error
		wantStatus int
		wantText   string
	}{
		{name: "no file", productID: auroraID, wantStatus: http.StatusSeeOther, wantText: "Escolha pelo menos uma foto"},
		{
			name: "storage down", productID: auroraID, files: map[string][]byte{"IMG_0001.JPG": []byte("photo")},
			photoErr: errors.New("r2 unreachable"), wantStatus: http.StatusSeeOther, wantText: "IMG_0001.JPG: não consegui salvar agora",
		},
		{
			name: "product gone", productID: "gone", files: map[string][]byte{"IMG_0001.JPG": []byte("photo")},
			wantStatus: http.StatusNotFound, wantText: "Peça não encontrada",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.login(t)
			app.products.add(aurora())
			app.products.photoErr = tt.photoErr

			res, body := app.postFiles(t, "/admin/products/"+tt.productID+"/photos", tt.files)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if res.StatusCode == http.StatusSeeOther {
				_, body = app.get(t, "/admin/products/"+auroraID, nil)
			}
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("the page does not say %q", tt.wantText)
			}
		})
	}
}

func TestMovePhoto(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(3))
	path := "/admin/products/" + auroraID + "/photos/img-3/move"

	res, _ := app.post(t, path, url.Values{"to": {"0"}})
	if res.Header.Get("Location") != "/admin/products/"+auroraID+"#fotos" {
		t.Errorf("location %q", res.Header.Get("Location"))
	}
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); !strings.Contains(body, "essa é a nova capa") {
		t.Error("no confirmation of the new cover")
	}
	app.post(t, path, url.Values{"to": {"1"}})
	// Every move says it was saved: there is no button to save the order.
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); !strings.Contains(body, "Ordem salva.") {
		t.Error("no confirmation after moving a photo")
	}
	if !slices.Equal(app.products.moves, []int{0, 1}) {
		t.Errorf("moves = %v, want [0 1]", app.products.moves)
	}

	app.post(t, path, url.Values{"to": {"primeiro"}})
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); !strings.Contains(body, "Não consegui ler o pedido") {
		t.Error("an unreadable position was not reported")
	}
	app.post(t, "/admin/products/"+auroraID+"/photos/img-9/move", url.Values{"to": {"0"}})
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); !strings.Contains(body, "Essa foto não existe mais") {
		t.Error("a missing photo was not reported")
	}
	if len(app.products.moves) != 2 {
		t.Errorf("moves = %v, want only the two valid ones", app.products.moves)
	}
}

func TestDeletePhoto(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(2))
	path := "/admin/products/" + auroraID + "/photos/img-1/delete"

	res, body := app.get(t, path, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `src="`+testImageBaseURL+`/produtos/img-1-600.jpg"`) ||
		!strings.Contains(body, "Ela é a capa") {
		t.Errorf("confirmation: status %d, want the photo shown and the cover warning", res.StatusCode)
	}
	if res, _ := app.get(t, "/admin/products/"+auroraID+"/photos/img-9/delete", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("confirmation for a missing photo: status %d, want 404", res.StatusCode)
	}

	res, _ = app.post(t, path, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin/products/"+auroraID+"#fotos" {
		t.Errorf("delete: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if n := len(app.products.products[auroraID].Images); n != 1 {
		t.Errorf("%d photos left, want 1", n)
	}

	app.products.photoErr = errors.New("database down")
	app.post(t, "/admin/products/"+auroraID+"/photos/img-2/delete", nil)
	if _, body := app.get(t, "/admin/products/"+auroraID, nil); !strings.Contains(body, "Não consegui apagar a foto agora") {
		t.Error("a failed delete was not reported")
	}
}

func TestPhotoRoutesRequireLogin(t *testing.T) {
	app := newTestApp(t, nil)
	app.products.add(auroraWithPhotos(1))
	for _, path := range []string{
		"/admin/products/" + auroraID + "/photos",
		"/admin/products/" + auroraID + "/photos/img-1/move",
		"/admin/products/" + auroraID + "/photos/img-1/delete",
	} {
		if res, _ := app.post(t, path, url.Values{"to": {"0"}}); res.Header.Get("Location") != "/login" {
			t.Errorf("anonymous POST %s: status %d, location %q", path, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if len(app.products.products[auroraID].Images) != 1 || len(app.products.moves) != 0 {
		t.Error("an anonymous request changed the photos")
	}
}
