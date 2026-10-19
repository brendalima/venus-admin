package rest

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

// notAPhoto is what the mock service refuses, as the processor would.
const notAPhoto = "not a photo"

// auroraInColours is Aurora in Bege, with photos img-1 to img-n, one P
// and two M, and in Vinho, with one photo and no stock counted.
func auroraInColours(n int) domain.Product {
	p := aurora()
	bege := domain.Colour{ID: "bege", ProductID: p.ID, Name: "Bege",
		Stock: []domain.Stock{{Size: "P", Quantity: 1}, {Size: "M", Quantity: 2}}}
	for i := range n {
		id := fmt.Sprintf("img-%d", i+1)
		bege.Images = append(bege.Images, domain.Image{ID: id, ColourID: bege.ID, Key600: "produtos/" + id + "-600.jpg"})
	}
	vinho := domain.Colour{ID: "vinho", ProductID: p.ID, Name: "Vinho", Position: 1,
		Images: []domain.Image{{ID: "img-v", ColourID: "vinho", Key600: "produtos/img-v-600.jpg"}}}
	p.Colours = []domain.Colour{bege, vinho}
	return p
}

// auroraForm is the form of auroraInColours as the edit page sends it,
// nothing changed, without JavaScript.
func auroraForm() url.Values {
	f := validForm()
	f[colourKeyField] = []string{"0", "1"}
	f.Set("colour_0_id", "bege")
	f.Set("colour_0_name", "Bege")
	f.Set("colour_0_stock_P", "1")
	f.Set("colour_0_stock_M", "2")
	f.Set("colour_1_id", "vinho")
	f.Set("colour_1_name", "Vinho")
	f.Set("colour_1_stock_P", "")
	f.Set("colour_1_stock_M", "")
	return f
}

// withJS adds what photos.js sends: each colour's photo order, by key.
func withJS(f url.Values, orders map[string]string) url.Values {
	for key, order := range orders {
		f.Set(photoOrderField(key), order)
	}
	return f
}

type upload struct {
	key  string // the colour block's
	name string
	data []byte
}

// postProduct sends the product form multipart, files last, as photos.js
// does. When the form carries a photo order, it is sent as photos.js
// sends it, with HX-Request, so the server names the next page instead
// of redirecting.
func (a *testApp) postProduct(t *testing.T, path string, fields url.Values, files ...upload) (*http.Response, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	js := false
	for k, vs := range fields {
		js = js || strings.HasPrefix(k, "photo_order")
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, f := range files {
		part, err := mw.CreateFormFile(photoFilesField(f.key), f.name)
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
	if js {
		req.Header.Set("HX-Request", "true")
	}
	return a.do(t, req)
}

func lastColourEdit(t *testing.T, app *testApp) *domain.ColoursEdit {
	t.Helper()
	if len(app.products.colourEdits) == 0 {
		t.Fatal("the service was not called")
	}
	return app.products.colourEdits[len(app.products.colourEdits)-1]
}

func photoIDs(c domain.Colour) []string {
	ids := []string{}
	for _, img := range c.Images {
		ids = append(ids, img.ID)
	}
	return ids
}

// region returns the element of body opening with the attribute attr,
// up to the start of the next colour block, for checking what one block
// shows.
func region(t *testing.T, body, attr string) string {
	t.Helper()
	i := strings.Index(body, attr)
	if i < 0 {
		t.Fatalf("no %s in the page", attr)
	}
	rest := body[i:]
	if j := strings.Index(rest[1:], "data-colour=\""); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

func TestEditPageShowsColours(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(3))

	_, body := app.get(t, "/admin/products/"+auroraID, nil)
	for _, want := range []string{
		"Cores e estoque",
		`enctype="multipart/form-data"`,
		`<script src="/static/photos.js?v=`,
		`data-next-key="2"`,
		`data-max-colours="6"`,
		// Keys are given in order, and say which colour each block is.
		`name="colour_key" value="0"`, `name="colour_0_id" value="bege"`, `name="colour_0_name" type="text" value="Bege"`,
		`name="colour_1_id" value="vinho"`,
		// Quantities as stored, a size the piece does not come in hidden.
		`name="colour_0_stock_P" type="text"`, `inputmode="numeric" value="1"`,
		`data-stock-size="G" hidden`,
		// Existing colours are deleted with a box, new ones dropped by photos.js.
		`name="colour_0_remove" type="checkbox"`, `Apagar esta cor e as fotos dela ao salvar`,
		`data-id="img-1" data-src="` + testImageBaseURL + `/produtos/img-1-600.jpg"`,
		`data-id="img-v"`,
		`name="photos_1" type="file"`,
		// photos.js copies this to add a colour.
		`<template data-colour-template>`, `name="colour___N___name"`,
		"Adicionar outra cor",
		// Without JavaScript, a blank block adds one.
		`<noscript>`, `name="colour_2_name"`,
		"mesmo que ainda haja quantidade cadastrada",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page lacks %s", want)
		}
	}
	// With JavaScript on, a noscript's content is text up to the first
	// </noscript>: one nested in another would end both, and the rest of
	// the block would break out and close the form before its button.
	for rest := body; strings.Contains(rest, "<noscript>"); {
		rest = rest[strings.Index(rest, "<noscript>")+len("<noscript>"):]
		if inner := rest[:strings.Index(rest, "</noscript>")]; strings.Contains(inner, "<noscript>") {
			t.Fatal("a noscript is nested in another")
		}
	}
	if strings.Contains(body, `name="colour_2_remove"`) {
		t.Error("a new colour offers to delete what it does not have")
	}
	if bege := region(t, body, `data-colour="0"`); strings.Contains(bege, "img-v") || !strings.Contains(bege, "img-3") {
		t.Error("the photos are not under their colour")
	}
}

func TestNewPageOffersOneColour(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	_, body := app.get(t, "/admin/products/new", nil)
	for _, want := range []string{`name="colour_key" value="0"`, `name="colour_0_name"`, `name="photos_0"`, `data-next-key="1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("new product page lacks %s", want)
		}
	}
	// The block on screen is blank already: no second one without JS.
	if strings.Contains(body, "<noscript>\n        <p class=\"hint\">Para cadastrar mais uma cor") || strings.Contains(body, `name="colour_1_name"`) {
		t.Error("the new product page offers two blank colours")
	}
	// The only colour cannot be dropped.
	if !regexp.MustCompile(`data-drop-colour\s+hidden`).MatchString(region(t, body, `data-colour="0"`)) {
		t.Error("the only colour offers to be dropped")
	}
}

func TestSaveWithColours(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(3))

	// Bege: img-3 becomes the cover, img-2 goes, a new photo goes second,
	// and M is no longer counted. Vinho is deleted. Verde is new, key 5
	// since photos.js never reuses keys, with three P and a photo.
	form := auroraForm()
	form[colourKeyField] = []string{"0", "1", "5"}
	form.Set("colour_0_stock_M", "")
	form.Set("colour_1_remove", "on")
	form.Set("colour_5_id", "")
	form.Set("colour_5_name", " Verde ")
	form.Set("colour_5_stock_P", "3")
	form.Set("colour_5_stock_M", "")
	form.Set(photoRemoveField("0"), "img-2")
	form = withJS(form, map[string]string{"0": "id:img-3,new:0,id:img-1", "1": "id:img-v", "5": "new:0"})

	res, _ := app.postProduct(t, "/admin/products/"+auroraID, form,
		upload{"0", "IMG_0001.JPG", []byte("bege")}, upload{"5", "IMG_0002.JPG", []byte("verde")})
	if res.StatusCode != http.StatusOK || res.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("save: status %d, HX-Redirect %q, want 200 naming /admin", res.StatusCode, res.Header.Get("HX-Redirect"))
	}

	edit := lastColourEdit(t, app)
	if edit == nil || len(edit.Colours) != 2 || !slices.Equal(edit.Removed, []string{"vinho"}) {
		t.Fatalf("edit %+v, want Bege and a new colour, Vinho removed", edit)
	}
	bege, verde := edit.Colours[0], edit.Colours[1]
	if bege.ID != "bege" || !slices.Equal(bege.Stock, []domain.Stock{{Size: "P", Quantity: 1}}) {
		t.Errorf("Bege %+v, want P counted alone", bege)
	}
	if want := []domain.PhotoSlot{{ImageID: "img-3"}, {NewIndex: 0}, {ImageID: "img-1"}}; !slices.Equal(bege.Photos.Order, want) ||
		!slices.Equal(bege.Photos.Removed, []string{"img-2"}) || bege.Photos.AppendNew {
		t.Errorf("Bege's photos %+v, want order %v and img-2 removed", bege.Photos, want)
	}
	if data, err := bege.Photos.New[0].Bytes(); err != nil || string(data) != "bege" {
		t.Errorf("Bege's new photo = %q, %v, want its own file", data, err)
	}
	if verde.ID != "" || verde.Name != " Verde " || !slices.Equal(verde.Stock, []domain.Stock{{Size: "P", Quantity: 3}}) {
		t.Errorf("new colour %+v", verde)
	}
	if data, err := verde.Photos.New[0].Bytes(); err != nil || string(data) != "verde" || len(verde.Photos.New) != 1 {
		t.Errorf("new colour's photo = %q, %v, want its own file", data, err)
	}

	stored := app.products.products[auroraID].Colours
	if len(stored) != 2 || stored[1].Name != "Verde" || !slices.Equal(photoIDs(stored[0]), []string{"img-3", "added-0-0", "img-1"}) {
		t.Errorf("stored colours %+v", stored)
	}
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "foram salvas") {
		t.Error("no confirmation after saving")
	}
}

func TestSaveWithoutJavaScript(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(1))

	// A photo for Bege, Vinho renamed, and the blank block the page
	// offers filled in: a new colour with its quantity and photo.
	form := auroraForm()
	form[colourKeyField] = []string{"0", "1", "2"}
	form.Set("colour_1_name", "Vinho Escuro")
	form.Set("colour_2_id", "")
	form.Set("colour_2_name", "Preto")
	form.Set("colour_2_stock_M", "4")
	res, _ := app.postProduct(t, "/admin/products/"+auroraID, form,
		upload{"0", "IMG_0001.JPG", []byte("one")}, upload{"2", "IMG_0002.JPG", []byte("two")})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("save: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	edit := lastColourEdit(t, app)
	if len(edit.Colours) != 3 {
		t.Fatalf("%d colours, want 3", len(edit.Colours))
	}
	for i, c := range edit.Colours {
		if !c.Photos.AppendNew {
			t.Errorf("colour %d: photos %+v, want new files after the existing ones", i, c.Photos)
		}
	}
	stored := app.products.products[auroraID].Colours
	if len(stored) != 3 || stored[1].Name != "Vinho Escuro" || stored[2].Name != "Preto" ||
		!slices.Equal(photoIDs(stored[0]), []string{"img-1", "added-0-0"}) || !slices.Equal(photoIDs(stored[1]), []string{"img-v"}) ||
		!slices.Equal(stored[2].Stock, []domain.Stock{{Size: "M", Quantity: 4}}) {
		t.Errorf("stored colours %+v", stored)
	}

	// The blank block left alone saves nothing more.
	app.products.add(auroraInColours(1))
	form = auroraForm()
	form[colourKeyField] = []string{"0", "1", "2"}
	form.Set("colour_2_name", "")
	if res, _ := app.postProduct(t, "/admin/products/"+auroraID, form); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save with a blank colour: status %d", res.StatusCode)
	}
	if n := len(app.products.products[auroraID].Colours); n != 2 {
		t.Errorf("%d colours after saving a blank one, want 2", n)
	}

	// A form without a colour section, which no page sends any more,
	// leaves the colours alone.
	plain := validForm()
	for k := range plain {
		if strings.HasPrefix(k, "colour_") {
			plain.Del(k)
		}
	}
	app.post(t, "/admin/products/"+auroraID, plain)
	if edit := lastColourEdit(t, app); edit != nil {
		t.Errorf("colour edit %+v, want nil", edit)
	}
}

func TestCreateWithColours(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	form := withJS(validForm(), map[string]string{"0": "new:0"})
	form.Set("colour_0_stock_P", "1")
	res, _ := app.postProduct(t, "/admin/products", form, upload{"0", "IMG_0001.JPG", []byte("one")})
	if res.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("create: status %d, HX-Redirect %q", res.StatusCode, res.Header.Get("HX-Redirect"))
	}
	c := app.products.created[0].Colours
	if len(c) != 1 || c[0].Name != "Bege" || len(c[0].Images) != 1 || !slices.Equal(c[0].Stock, []domain.Stock{{Size: "P", Quantity: 1}}) {
		t.Errorf("created with colours %+v", c)
	}
}

func TestSaveWithColoursRefused(t *testing.T) {
	big := upload{"1", "enorme.png", bytes.Repeat([]byte("x"), maxPhotoBytes+1)}
	tests := []struct {
		name        string
		edit        func(url.Values)
		files       []upload
		photoErr    error
		wantStatus  int
		wantTexts   []string
		wantIn      map[string]string // a text the region opening with the attribute must hold
		wantService bool              // whether the service got the request
	}{
		{
			name:       "a file of the second colour is not a photo",
			files:      []upload{{"0", "IMG_0001.JPG", []byte("one")}, {"1", "IMG_0002.HEIC", []byte(notAPhoto)}},
			wantStatus: http.StatusBadRequest, wantService: true,
			wantTexts: []string{"Nada foi salvo"},
			wantIn:    map[string]string{`data-photo-errors="1"`: "IMG_0002.HEIC: Esse arquivo não é uma foto em JPEG, PNG ou WebP."},
		},
		{
			name:       "fields, a colour and a photo at once",
			edit:       func(f url.Values) { f.Set("name", ""); f.Set("colour_1_name", "bege") },
			files:      []upload{{"1", "IMG_0002.HEIC", []byte(notAPhoto)}},
			wantStatus: http.StatusBadRequest, wantService: true,
			wantTexts: []string{`id="name-error"`},
			wantIn: map[string]string{
				`data-colour-fields="1"`: "Já existe outra cor com esse nome nesta peça",
				`data-photo-errors="1"`:  "IMG_0002.HEIC: Esse arquivo",
			},
		},
		{
			// Dropped, an unreadable quantity would read as "not
			// counted": the service is not even called, but the rest is
			// still checked.
			name:       "an unreadable quantity",
			edit:       func(f url.Values) { f.Set("colour_0_stock_M", "dois"); f.Set("colour_1_name", "") },
			wantStatus: http.StatusBadRequest,
			wantIn: map[string]string{
				`data-colour-fields="0"`: "M: Escreva a quantidade só com números, ou deixe vazio",
				`data-colour-fields="1"`: "Falta o nome da cor",
			},
			wantTexts: []string{`value="dois"`},
		},
		{
			name:       "a quantity out of bounds",
			edit:       func(f url.Values) { f.Set("colour_0_stock_P", "1000") },
			wantStatus: http.StatusBadRequest, wantService: true,
			wantIn: map[string]string{`data-colour-fields="0"`: "P: A quantidade passa de 999"},
		},
		{
			name:       "every colour deleted",
			edit:       func(f url.Values) { f.Set("colour_0_remove", "on"); f.Set("colour_1_remove", "on") },
			wantStatus: http.StatusBadRequest, wantService: true,
			wantIn: map[string]string{`data-colour-errors`: "Cadastre ao menos uma cor"},
		},
		{
			name: "a file over the size limit", files: []upload{big},
			wantStatus: http.StatusBadRequest,
			wantIn:     map[string]string{`data-photo-errors="1"`: "enorme.png: passa de 15 MB."},
		},
		{
			name: "an unreadable photo list", edit: func(f url.Values) { f.Set(photoOrderField("0"), "id:img-1,primeira") },
			wantStatus: http.StatusBadRequest, wantTexts: []string{"Não consegui entender a lista de cores e fotos"},
		},
		{
			name: "an unreadable colour key", edit: func(f url.Values) { f[colourKeyField] = []string{"0", "um"} },
			wantStatus: http.StatusBadRequest, wantTexts: []string{"Não consegui entender a lista de cores e fotos"},
		},
		{
			name: "photos changed elsewhere", edit: func(f url.Values) { f.Set(photoOrderField("0"), "id:img-1,id:img-9") },
			wantStatus: http.StatusConflict, wantService: true, wantTexts: []string{"As cores ou as fotos desta peça mudaram enquanto você editava"},
		},
		{
			name: "a colour added elsewhere", edit: func(f url.Values) { f[colourKeyField] = []string{"0"} },
			wantStatus: http.StatusConflict, wantService: true, wantTexts: []string{"As cores ou as fotos desta peça mudaram"},
		},
		{
			name:       "storage down",
			files:      []upload{{"0", "IMG_0001.JPG", []byte("one")}},
			photoErr:   errors.New("r2 unreachable"),
			wantStatus: http.StatusInternalServerError, wantService: true,
			wantTexts: []string{"Não consegui salvar agora. Nada foi salvo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.login(t)
			app.products.add(auroraInColours(1))
			app.products.photoErr = tt.photoErr
			form := withJS(auroraForm(), map[string]string{"0": "id:img-1", "1": "id:img-v"})
			form.Set("name", "Biquíni Aurora Renomeado")
			for _, f := range tt.files {
				form.Set(photoOrderField(f.key), form.Get(photoOrderField(f.key))+",new:0")
			}
			if tt.edit != nil {
				tt.edit(form)
			}

			res, body := app.postProduct(t, "/admin/products/"+auroraID, form, tt.files...)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			for _, want := range tt.wantTexts {
				if !strings.Contains(body, want) {
					t.Errorf("the answer does not say %q", want)
				}
			}
			for attr, want := range tt.wantIn {
				if !strings.Contains(region(t, body, attr), want) {
					t.Errorf("the region %s does not say %q", attr, want)
				}
			}
			// photos.js swaps these regions in from the answer.
			for _, r := range []string{"data-form-messages", "data-fields", "data-colour-errors", `data-colour-fields="0"`, `data-photo-errors="1"`} {
				if !strings.Contains(body, r) {
					t.Errorf("the answer lacks the %s region", r)
				}
			}
			if got := len(app.products.colourEdits) > 0; got != tt.wantService {
				t.Errorf("service called: %v, want %v", got, tt.wantService)
			}
			if p := app.products.products[auroraID]; p.Name != "Biquíni Aurora" || len(p.Images()) != 2 || len(p.Colours) != 2 {
				t.Errorf("something was saved: %q with %d colours and %d photos", p.Name, len(p.Colours), len(p.Images()))
			}
		})
	}
}

// A page loaded before colours were on the form sends its photos under
// names nobody reads any more. Saving the rest would drop them silently.
func TestSaveFromAnOutdatedPage(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(1))

	old := url.Values{"name": {"Biquíni Aurora"}, "sizes": {"P"}, "photo_order": {"id:img-1"}}
	res, body := app.postProduct(t, "/admin/products/"+auroraID, old)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Esta página está desatualizada") {
		t.Errorf("status %d, want 400 asking to reload", res.StatusCode)
	}
	if len(app.products.colourEdits) != 0 {
		t.Error("the service was asked to save it")
	}
}

func TestSaveWithColoursOfAProductGoneMeanwhile(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)

	res, _ := app.postProduct(t, "/admin/products/"+auroraID, withJS(validForm(), map[string]string{"0": "new:0"}),
		upload{"0", "IMG_0001.JPG", []byte("one")})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", res.StatusCode)
	}
}

func TestProductListShowsStock(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(1))
	plain := aurora()
	plain.ID, plain.Name = "plain", "Maiô Órion"
	plain.Colours = []domain.Colour{{ID: "unica", Name: "Única"}}
	app.products.add(plain)

	_, body := app.get(t, "/admin", nil)
	aurora := body[strings.Index(body, `id="product-`+auroraID+`"`):]
	for _, want := range []string{
		"· 3 em estoque",
		`action="/admin/products/` + auroraID + `/colours/bege/stock/P"`,
		// P has 1 and M 2: each button sends what it leaves.
		`name="quantity" value="0"`, `name="quantity" value="1"`, `name="quantity" value="2"`, `name="quantity" value="3"`,
		`aria-label="Tirar uma peça de Bege, tamanho M"`,
		`aria-label="Pôr mais uma peça de Bege, tamanho M"`,
	} {
		if !strings.Contains(aurora, want) {
			t.Errorf("Aurora's row lacks %s", want)
		}
	}
	// Vinho's stock is not counted: nothing to take from.
	if strings.Contains(aurora, "/colours/vinho/") {
		t.Error("a colour without stock offers buttons")
	}
	orion := body[strings.Index(body, `id="product-plain"`):]
	orion = orion[:strings.Index(orion, "</li>")]
	if strings.Contains(orion, "em estoque") || strings.Contains(orion, "stock-list") {
		t.Error("a piece whose stock is not counted shows a count")
	}
}

func TestProductSetStock(t *testing.T) {
	app := newTestApp(t, nil)
	app.login(t)
	app.products.add(auroraInColours(1))
	path := "/admin/products/" + auroraID + "/colours/bege/stock/"

	// Through HTMX: the row alone, with what is left. Twice, as a double
	// tap would: the same quantity both times, so one piece is taken.
	for range 2 {
		res, body := app.postHTMX(t, path+"M", url.Values{"quantity": {"1"}})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if !strings.HasPrefix(strings.TrimSpace(body), "<li") || strings.Contains(body, "<html") || !strings.Contains(body, "· 2 em estoque") {
			t.Errorf("want only the row with 2 left, got:\n%s", body)
		}
	}
	if q := app.products.products[auroraID].Colours[0].Quantity("M"); q != 1 {
		t.Errorf("M = %d after a double tap, want 1", q)
	}

	// The last one: − is off, + still there to restock.
	_, body := app.postHTMX(t, path+"P", url.Values{"quantity": {"0"}})
	if !strings.Contains(body, `tamanho P" disabled>−</button>`) || strings.Contains(body, `tamanho P" disabled>+</button>`) {
		t.Errorf("at zero, want − disabled and + enabled, got:\n%s", body)
	}
	res, body := app.postHTMX(t, path+"P", url.Values{"quantity": {"1"}})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "· 2 em estoque") {
		t.Errorf("+: status %d, want the row with 2 left", res.StatusCode)
	}

	// Without JavaScript: back to the list.
	res, _ = app.post(t, path+"M", url.Values{"quantity": {"0"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Errorf("plain form: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}

	if res, _ := app.post(t, path+"M", url.Values{"quantity": {"menos um"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unreadable quantity: status %d, want 400", res.StatusCode)
	}

	failures := []struct {
		name, path, want string
	}{
		{"a size the piece no longer has", path + "GG", "Essa peça não tem o tamanho GG. Nada foi salvo"},
		{"a colour gone", "/admin/products/" + auroraID + "/colours/verde/stock/P", "Essa peça ou essa cor não existe mais"},
		{"a piece gone", "/admin/products/gone/colours/bege/stock/P", "Essa peça ou essa cor não existe mais"},
	}
	for _, f := range failures {
		res, _ := app.postHTMX(t, f.path, url.Values{"quantity": {"0"}})
		if res.Header.Get("HX-Redirect") != "/admin" {
			t.Errorf("%s: HX-Redirect %q, want /admin", f.name, res.Header.Get("HX-Redirect"))
		}
		if _, body := app.get(t, "/admin", nil); !strings.Contains(body, f.want) {
			t.Errorf("%s: the list does not say %q", f.name, f.want)
		}
	}

	app.products.writeErr = errors.New("database timeout")
	app.postHTMX(t, path+"P", url.Values{"quantity": {"0"}})
	if _, body := app.get(t, "/admin", nil); !strings.Contains(body, "Não consegui salvar agora") || strings.Contains(body, "database timeout") {
		t.Error("a failure is not explained, or leaks the error")
	}
}

func TestStockRequiresLogin(t *testing.T) {
	app := newTestApp(t, nil)
	app.products.add(auroraInColours(1))

	res, _ := app.post(t, "/admin/products/"+auroraID+"/colours/bege/stock/P", url.Values{"quantity": {"0"}})
	if res.Header.Get("Location") != "/login" || len(app.products.stockSets) != 0 {
		t.Errorf("anonymous −1: status %d, stock set %v", res.StatusCode, app.products.stockSets)
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
