package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func TestNewAPIRejectsMissingDependencies(t *testing.T) {
	full := APIInput{Service: newMockProductService(), Token: "t", ImageBaseURL: testImageBaseURL}
	for name, in := range map[string]APIInput{
		"service":        {Token: full.Token, ImageBaseURL: full.ImageBaseURL},
		"token":          {Service: full.Service, ImageBaseURL: full.ImageBaseURL},
		"image base url": {Service: full.Service, Token: full.Token},
	} {
		if _, err := NewAPI(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
}

func TestAPIRequiresToken(t *testing.T) {
	app := newTestApp(t, nil)
	app.products.add(aurora())

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{name: "no token", want: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer nope", want: http.StatusUnauthorized},
		{name: "token as a prefix", header: "Bearer " + testAPIToken[:5], want: http.StatusUnauthorized},
		{name: "token without the scheme", header: testAPIToken, want: http.StatusUnauthorized},
		{name: "right token", header: "Bearer " + testAPIToken, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.header != "" {
				header.Set("Authorization", tt.header)
			}
			res, body := app.get(t, "/api/products", header)
			if res.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.want)
			}
			if tt.want == http.StatusUnauthorized && body != "{\"error\":\"missing or invalid token\"}\n" {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestAPIProducts(t *testing.T) {
	app := newTestApp(t, nil)
	auth := http.Header{"Authorization": {"Bearer " + testAPIToken}}

	res, body := app.get(t, "/api/products", auth)
	if res.StatusCode != http.StatusOK || body != "[]\n" {
		t.Fatalf("empty catalogue: status %d, body %q", res.StatusCode, body)
	}

	withPhotos := aurora()
	withPhotos.Description = "Verde-água"
	withPhotos.CollectionID, withPhotos.CollectionName = "verao", "Verão 2026"
	withPhotos.Details = []string{"Forro duplo"}
	withPhotos.Sizes = []string{"P", "M", "G"}
	// Two colours, one counting stock with M missing and G stored at zero,
	// one not counting. Each colour carries its own photos; there is no
	// flat list at the product level any more.
	withPhotos.Colours = []domain.Colour{
		{ID: "c1", Name: "Bege", Hexes: []string{"#c8a27c", "#ffffff"}, Images: []domain.Image{{ID: "i1", Key600: "produtos/i1-600.jpg", Key1200: "produtos/i1-1200.jpg"}},
			Stock: []domain.Stock{{Size: "P", Quantity: 1}, {Size: "G", Quantity: 0}}},
		{ID: "c2", Name: "Vinho", Images: []domain.Image{{ID: "i2", Key600: "produtos/i2-600.jpg", Key1200: "produtos/i2-1200.jpg"}}},
	}
	// Details empty but not nil, as the repository returns them.
	unpriced := domain.Product{ID: "other", Slug: "maio-orion", Name: "Maiô Órion", Sizes: []string{"M"}, Details: []string{}, SoldOut: true}
	// Archived: out of the collection, so never sent to the site.
	archived := domain.Product{ID: "gone", Slug: "saida-lua", Name: "Saída Lua", Sizes: []string{"P"}, ArchivedAt: time.Now()}
	app.products.add(withPhotos)
	app.products.add(unpriced)
	app.products.add(archived)

	res, body = app.get(t, "/api/products", auth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content type = %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q", got)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, body)
	}
	if len(got) != 2 {
		t.Fatalf("got %d products, want 2", len(got))
	}
	a, o := got[0], got[1] // the mock lists by name

	if a["price_cents"] != float64(18990) || a["slug"] != "biquini-aurora" || a["sold_out"] != false {
		t.Errorf("aurora = %v", a)
	}

	// Every size of the grid for the colour that counts, zeros included:
	// a zero left out would read as not counted rather than unavailable.
	var wantColours any
	if err := json.Unmarshal([]byte(`[
		{"id": "c1", "name": "Bege", "hex": ["#c8a27c", "#ffffff"], "tracks_stock": true,
		 "images": [{"url_600": "`+testImageBaseURL+`/produtos/i1-600.jpg", "url_1200": "`+testImageBaseURL+`/produtos/i1-1200.jpg"}],
		 "stock": [{"size": "P", "quantity": 1}, {"size": "M", "quantity": 0}, {"size": "G", "quantity": 0}]},
		{"id": "c2", "name": "Vinho", "hex": [], "tracks_stock": false,
		 "images": [{"url_600": "`+testImageBaseURL+`/produtos/i2-600.jpg", "url_1200": "`+testImageBaseURL+`/produtos/i2-1200.jpg"}],
		 "stock": []}
	]`), &wantColours); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a["colours"], wantColours) {
		t.Errorf("aurora colours = %v\nwant %v", a["colours"], wantColours)
	}

	// No price must not reach the site as R$ 0,00: the field is absent.
	if _, has := o["price_cents"]; has {
		t.Errorf("a product without a price has price_cents: %v", o)
	}
	if want := map[string]any{"id": "verao", "name": "Verão 2026"}; !reflect.DeepEqual(a["collection"], want) {
		t.Errorf("aurora collection = %v, want %v", a["collection"], want)
	}
	// A piece in no collection says so, rather than leaving it out.
	if c, has := o["collection"]; !has || c != nil {
		t.Errorf("orion collection = %v (present %v), want null", c, has)
	}
	for _, field := range []string{"description", "details"} {
		if _, has := o[field]; has {
			t.Errorf("empty %s is sent: %v", field, o)
		}
	}
	if colours, ok := o["colours"].([]any); !ok || len(colours) != 0 {
		t.Errorf("orion colours = %v", o["colours"])
	}
	if o["sold_out"] != true {
		t.Errorf("orion = %v", o)
	}
	// The flat images list is gone: the site reads each colour's photos.
	// Sending it again would put two sources of truth back in the
	// response, and the site would ignore one of them.
	for _, p := range got {
		if _, has := p["images"]; has {
			t.Errorf("%s still has a flat images list: %v", p["slug"], p["images"])
		}
	}

	app.products.listErr = errors.New("database timeout")
	res, body = app.get(t, "/api/products", auth)
	if res.StatusCode != http.StatusInternalServerError || body != "{\"error\":\"internal error\"}\n" {
		t.Errorf("list failure: status %d, body %q", res.StatusCode, body)
	}
}
