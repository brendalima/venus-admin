package rest

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/core/services"
)

// productResponse is a product as the site's build reads it: only what
// the site shows. A product without a price has no price_cents, so the
// site can never show it as R$ 0,00.
type productResponse struct {
	ID         string           `json:"id"`
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	PriceCents int64            `json:"price_cents,omitzero"`
	Sizes      []string         `json:"sizes"`
	Colours    []colourResponse `json:"colours"`
	// Images flattens every colour's photos, in colour order, for a site
	// that does not read colours yet.
	Images      []imageResponse `json:"images"`
	Description string          `json:"description,omitempty"`
	Details     []string        `json:"details,omitempty"`
	SoldOut     bool            `json:"sold_out"`
}

// colourResponse is one colour with its photos, cover first, and its
// stock. A colour that tracks stock lists every size of the product, in
// the product's order, an uncounted size at zero, so the site never has
// to tell a missing size from an unavailable one. One that does not track
// stock has none.
type colourResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Images      []imageResponse `json:"images"`
	TracksStock bool            `json:"tracks_stock"`
	Stock       []stockResponse `json:"stock"`
}

// stockResponse never omits a zero quantity: a size gone missing would
// read as not counted instead of unavailable.
type stockResponse struct {
	Size     string `json:"size"`
	Quantity int32  `json:"quantity"`
}

type imageResponse struct {
	URL600  string `json:"url_600"`
	URL1200 string `json:"url_1200"`
}

type APIInput struct {
	Service services.ProductService
	// Token is what the site's build sends as "Authorization: Bearer".
	Token string
	// ImageBaseURL is where the photos are public; keys are appended.
	ImageBaseURL string
}

// API serves what the static site's build reads. Visitors never reach
// it: the site is rebuilt from it on each publish.
type API struct {
	service      services.ProductService
	tokenHash    [sha256.Size]byte
	imageBaseURL string
}

func NewAPI(in APIInput) (*API, error) {
	switch {
	case in.Service == nil:
		return nil, errors.New("missing product service dependency")
	case in.Token == "":
		return nil, errors.New("missing api token")
	case in.ImageBaseURL == "":
		return nil, errors.New("missing image base url")
	}
	return &API{service: in.Service, tokenHash: sha256.Sum256([]byte(in.Token)), imageBaseURL: in.ImageBaseURL}, nil
}

func MustNewAPI(in APIInput) *API {
	a, err := NewAPI(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create api handler: %w", err))
	}
	return a
}

// RequireToken lets through only requests carrying the token. Both sides
// are hashed before comparing, so the comparison takes the same time
// whatever was sent, its length included.
func (a *API) RequireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sent := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(sent[:], a.tokenHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) Products(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	products, err := a.service.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "couldn't list products for the api", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	res := make([]productResponse, 0, len(products))
	for _, p := range products {
		res = append(res, a.toProductResponse(p))
	}
	// Read once per build, and always wanted fresh.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (a *API) toProductResponse(p domain.Product) productResponse {
	res := productResponse{
		ID:          p.ID,
		Slug:        p.Slug,
		Name:        p.Name,
		Sizes:       p.Sizes,
		Colours:     make([]colourResponse, 0, len(p.Colours)),
		Images:      a.toImageResponses(p.Images()),
		Description: p.Description,
		Details:     p.Details,
		SoldOut:     p.SoldOut,
	}
	if p.HasPrice() {
		res.PriceCents = p.PriceCents
	}
	if res.Sizes == nil {
		res.Sizes = []string{}
	}
	for _, c := range p.Colours {
		res.Colours = append(res.Colours, a.toColourResponse(c, res.Sizes))
	}
	return res
}

func (a *API) toColourResponse(c domain.Colour, sizes []string) colourResponse {
	res := colourResponse{
		ID:          c.ID,
		Name:        c.Name,
		Images:      a.toImageResponses(c.Images),
		TracksStock: c.TracksStock(),
		Stock:       []stockResponse{},
	}
	if !res.TracksStock {
		return res
	}
	for _, size := range sizes {
		res.Stock = append(res.Stock, stockResponse{Size: size, Quantity: c.Quantity(size)})
	}
	return res
}

func (a *API) toImageResponses(images []domain.Image) []imageResponse {
	res := make([]imageResponse, 0, len(images))
	for _, img := range images {
		res = append(res, imageResponse{
			URL600:  a.imageBaseURL + "/" + img.Key600,
			URL1200: a.imageBaseURL + "/" + img.Key1200,
		})
	}
	return res
}
