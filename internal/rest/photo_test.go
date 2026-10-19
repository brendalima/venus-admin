package rest

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// notAPhoto is what the mock service refuses, as the processor would.
const notAPhoto = "not a photo"

func auroraWithPhotos(n int) domain.Product {
	p := aurora()
	c := domain.Colour{ID: "bege", ProductID: p.ID, Name: "Bege"}
	for i := range n {
		id := fmt.Sprintf("img-%d", i+1)
		c.Images = append(c.Images, domain.Image{ID: id, ColourID: c.ID, Key600: "produtos/" + id + "-600.jpg"})
	}
	p.Colours = []domain.Colour{c}
	return p
}

type upload struct {
	name string
	data []byte
}

// postProduct sends the product form as photos.js does: multipart, with
// the files in order, and HX-Request so the server names the next page
// instead of redirecting. Pass order as nil to send the form as a browser
// without JavaScript would.
func (a *testApp) postProduct(t *testing.T, path string, fields url.Values, order *string, remove []string, files ...upload) (*http.Response, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if order != nil {
		if err := mw.WriteField(photoOrderField, *order); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range remove {
		if err := mw.WriteField(photoRemoveField, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		part, err := mw.CreateFormFile(photoFilesField, f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(f.data); err != nil {
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
	if order != nil {
		req.Header.Set("HX-Request", "true")
	}
	return a.do(t, req)
}

func ptr(s string) *string { return &s }

func lastEdit(t *testing.T, app *testApp) *domain.PhotoEdit {
	t.Helper()
	if len(app.products.edits) == 0 {
		t.Fatal("the service was not called")
	}
	return app.products.edits[len(app.products.edits)-1]
}

func TestEditPageShowsPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(3))

	_, body := app.get(t, "/admin/products/"+auroraID, nil)
	for _, want := range []string{
		`data-id="img-1" data-src="` + testImageBaseURL + `/produtos/img-1-600.jpg"`,
		`>Capa<`,
		`enctype="multipart/form-data"`,
		`data-product-form`,
		`<script src="/static/photos.js?v=`,
		`accept="image/jpeg,image/png,image/webp"`,
		`Nada muda nas fotos até você clicar em “Salvar alterações”`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page lacks %s", want)
		}
	}
	// Photos are no longer changed one click at a time.
	for _, gone := range []string{"/move", "/photos/img-1/delete"} {
		if strings.Contains(body, gone) {
			t.Errorf("edit page still links to %s", gone)
		}
	}
}

func TestSaveWithPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(3))

	// img-3 becomes the cover, img-2 goes, two new photos are placed.
	res, _ := app.postProduct(t, "/admin/products/"+auroraID, validForm(),
		ptr("id:img-3,new:1,id:img-1,new:0"), []string{"img-2"},
		upload{"IMG_0001.JPG", []byte("one")}, upload{"IMG_0002.JPG", []byte("two")})
	if res.StatusCode != http.StatusOK || res.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("save: status %d, HX-Redirect %q, want 200 naming /admin", res.StatusCode, res.Header.Get("HX-Redirect"))
	}

	edit := lastEdit(t, app)
	want := []domain.PhotoSlot{{ImageID: "img-3"}, {NewIndex: 1}, {ImageID: "img-1"}, {NewIndex: 0}}
	if edit == nil || !slices.Equal(edit.Order, want) || !slices.Equal(edit.Removed, []string{"img-2"}) || edit.AppendNew {
		t.Fatalf("edit = %+v, want order %v and img-2 removed", edit, want)
	}
	if len(edit.New) != 2 {
		t.Fatalf("%d new photos, want 2", len(edit.New))
	}
	if data, err := edit.New[1].Bytes(); err != nil || string(data) != "two" {
		t.Errorf("second new photo = %q, %v", data, err)
	}

	got := []string{}
	for _, img := range app.products.products[auroraID].Images() {
		got = append(got, img.ID)
	}
	if !slices.Equal(got, []string{"img-3", "added-1", "img-1", "added-0"}) {
		t.Errorf("photos %v", got)
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "foram salvas") {
		t.Error("no confirmation after saving")
	}
}

func TestSaveWithoutJavaScript(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(1))

	res, _ := app.postProduct(t, "/admin/products/"+auroraID, validForm(), nil, nil, upload{"IMG_0001.JPG", []byte("one")})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("save: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if edit := lastEdit(t, app); edit == nil || !edit.AppendNew || len(edit.New) != 1 {
		t.Errorf("edit = %+v, want the file appended after the existing photos", edit)
	}

	// No files and no photo list: the photos are left alone.
	app.postProduct(t, "/admin/products/"+auroraID, validForm(), nil, nil)
	if edit := lastEdit(t, app); edit != nil {
		t.Errorf("edit = %+v, want nil", edit)
	}
}

func TestCreateWithPhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	res, _ := app.postProduct(t, "/admin/products", validForm(), ptr("new:0"), nil, upload{"IMG_0001.JPG", []byte("one")})
	if res.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("create: status %d, HX-Redirect %q", res.StatusCode, res.Header.Get("HX-Redirect"))
	}
	if n := len(app.products.created[0].Images()); n != 1 {
		t.Errorf("created with %d photos, want 1", n)
	}
}

func TestSaveWithPhotosRefused(t *testing.T) {
	big := upload{"enorme.png", bytes.Repeat([]byte("x"), maxPhotoBytes+1)}
	tests := []struct {
		name        string
		edit        func(url.Values)
		order       string
		files       []upload
		photoErr    error
		wantStatus  int
		wantTexts   []string
		wantService bool // whether the service got the request
	}{
		{
			name:       "a file is not a photo",
			order:      "id:img-1,new:0,new:1",
			files:      []upload{{"IMG_0001.JPG", []byte("one")}, {"IMG_0002.HEIC", []byte(notAPhoto)}},
			wantStatus: http.StatusBadRequest, wantService: true,
			wantTexts: []string{"Nada foi salvo", "IMG_0002.HEIC: Esse arquivo não é uma foto em JPEG, PNG ou WebP."},
		},
		{
			name:       "a field and a photo at once",
			edit:       func(f url.Values) { f.Set("name", "") },
			order:      "id:img-1,new:0",
			files:      []upload{{"IMG_0002.HEIC", []byte(notAPhoto)}},
			wantStatus: http.StatusBadRequest, wantService: true,
			wantTexts: []string{`id="name-error"`, "IMG_0002.HEIC: Esse arquivo"},
		},
		{
			name: "a file over the size limit", order: "id:img-1,new:0", files: []upload{big},
			wantStatus: http.StatusBadRequest, wantTexts: []string{"enorme.png: passa de 15 MB."},
		},
		{
			name: "an unreadable photo list", order: "id:img-1,primeira",
			wantStatus: http.StatusBadRequest, wantTexts: []string{"Não consegui entender a lista de fotos"},
		},
		{
			name: "photos changed elsewhere", order: "id:img-1,id:img-9",
			wantStatus: http.StatusConflict, wantService: true, wantTexts: []string{"As fotos desta peça mudaram enquanto você editava"},
		},
		{
			name: "storage down", order: "id:img-1,new:0", files: []upload{{"IMG_0001.JPG", []byte("one")}},
			photoErr: errors.New("r2 unreachable"), wantStatus: http.StatusInternalServerError, wantService: true,
			wantTexts: []string{"Não consegui salvar agora. Nada foi salvo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.login(t)
			app.products.add(auroraWithPhotos(1))
			app.products.photoErr = tt.photoErr
			form := validForm()
			form.Set("name", "Biquíni Aurora Renomeado")
			if tt.edit != nil {
				tt.edit(form)
			}

			res, body := app.postProduct(t, "/admin/products/"+auroraID, form, ptr(tt.order), nil, tt.files...)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			for _, want := range tt.wantTexts {
				if !strings.Contains(body, want) {
					t.Errorf("the answer does not say %q", want)
				}
			}
			// photos.js swaps these regions in from the answer.
			for _, region := range []string{"data-form-messages", "data-fields", "data-photo-errors"} {
				if !strings.Contains(body, region) {
					t.Errorf("the answer lacks the %s region", region)
				}
			}
			if got := len(app.products.edits) > 0; got != tt.wantService {
				t.Errorf("service called: %v, want %v", got, tt.wantService)
			}
			if p := app.products.products[auroraID]; p.Name != "Biquíni Aurora" || len(p.Images()) != 1 {
				t.Errorf("something was saved: %q with %d photos", p.Name, len(p.Images()))
			}
		})
	}
}

func TestPhotosScriptIsServed(t *testing.T) {
	app := newTestApp(t, nil)
	src := assetURL("photos.js")
	if !strings.HasPrefix(src, "/static/photos.js?v=") {
		t.Fatalf("asset URL %q", src)
	}
	res, body := app.get(t, src, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "data-product-form") {
		t.Fatalf("GET %s: status %d", src, res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestPhotoPreviewsAllowedByCSP(t *testing.T) {
	w := httptest.NewRecorder()
	SecureHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data: blob:") {
		t.Errorf("CSP %q does not allow blob: previews", csp)
	}
}

// Until the form shows colours, it edits the first colour's photos and
// must leave every colour's name and stock as they are.
func TestSaveKeepsColoursTheFormDoesNotShow(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	p := auroraWithPhotos(1)
	p.Colours[0].Stock = []domain.Stock{{Size: "P", Quantity: 2}}
	p.Colours = append(p.Colours, domain.Colour{
		ID: "vinho", Name: "Vinho", Stock: []domain.Stock{{Size: "P", Quantity: 0}},
		Images: []domain.Image{{ID: "img-v", ColourID: "vinho"}},
	})
	app.products.add(p)

	app.postProduct(t, "/admin/products/"+auroraID, validForm(), ptr("new:0,id:img-1"), nil, upload{"IMG_0001.JPG", []byte("one")})

	edit := app.products.colourEdits[len(app.products.colourEdits)-1]
	if edit == nil || len(edit.Colours) != 2 || len(edit.Removed) != 0 {
		t.Fatalf("colour edit %+v, want both colours kept", edit)
	}
	bege, vinho := edit.Colours[0], edit.Colours[1]
	if bege.ID != "bege" || bege.Name != "Bege" || !slices.Equal(bege.Stock, p.Colours[0].Stock) {
		t.Errorf("first colour %+v, want Bege as stored", bege)
	}
	if want := []domain.PhotoSlot{{NewIndex: 0}, {ImageID: "img-1"}}; !slices.Equal(bege.Photos.Order, want) || len(bege.Photos.New) != 1 {
		t.Errorf("first colour's photos %+v, want the form's edit", bege.Photos)
	}
	if vinho.ID != "vinho" || vinho.Name != "Vinho" || !slices.Equal(vinho.Stock, p.Colours[1].Stock) {
		t.Errorf("second colour %+v, want Vinho as stored", vinho)
	}
	if !vinho.Photos.AppendNew || len(vinho.Photos.New) != 0 {
		t.Errorf("second colour's photos %+v, want them left as they are", vinho.Photos)
	}

	// Without a photo edit, the colours are not sent at all.
	app.postProduct(t, "/admin/products/"+auroraID, validForm(), nil, nil)
	if edit := app.products.colourEdits[len(app.products.colourEdits)-1]; edit != nil {
		t.Errorf("colour edit %+v, want nil", edit)
	}
}

func TestCreateGivesOneColour(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	app.postProduct(t, "/admin/products", validForm(), nil, nil)
	app.postProduct(t, "/admin/products", validForm(), ptr("new:0"), nil, upload{"IMG_0001.JPG", []byte("one")})

	for i, edit := range app.products.colourEdits {
		if edit == nil || len(edit.Colours) != 1 || edit.Colours[0].ID != "" || edit.Colours[0].Name != "Única" {
			t.Errorf("create %d: colour edit %+v, want one new colour named Única", i, edit)
		}
	}
	if n := len(app.products.colourEdits[1].Colours[0].Photos.New); n != 1 {
		t.Errorf("%d new photos in the colour, want 1", n)
	}
}

func TestSaveWithPhotosOfAProductGoneMeanwhile(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	res, _ := app.postProduct(t, "/admin/products/"+auroraID, validForm(), ptr("new:0"), nil, upload{"IMG_0001.JPG", []byte("one")})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", res.StatusCode)
	}
	if len(app.products.edits) != 0 {
		t.Error("the service was asked to save it")
	}
}

// The form has no place for colour errors yet. They must still be shown,
// or the answer would say to check the marked fields and mark none.
func TestColourErrorsShowWithThePhotos(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraWithPhotos(1))
	app.products.writeErr = domain.NewValidationError("colours", "Já existe outra cor com esse nome nesta peça")

	res, body := app.postProduct(t, "/admin/products/"+auroraID, validForm(), ptr("id:img-1"), nil)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Já existe outra cor com esse nome nesta peça") {
		t.Errorf("status %d, and the colour error is not shown", res.StatusCode)
	}
}
