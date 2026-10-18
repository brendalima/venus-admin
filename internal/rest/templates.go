package rest

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// The screens are embedded in the binary: there is no build step for the
// admin and nothing to keep in sync at deploy time.
//
//go:embed templates/*.html
var templatesFS embed.FS

// Templates holds one parsed set per page, each combining the page with
// the shared layout. They are parsed once, at startup, so a broken
// template stops the deploy instead of failing a request.
type Templates struct {
	pages map[string]*template.Template
}

// NewTemplates parses every page. imageBaseURL is where stored photos are
// served from; templates turn a photo's key into its address with
// imageURL.
func NewTemplates(imageBaseURL string) (*Templates, error) {
	if imageBaseURL == "" {
		return nil, errors.New("missing image base url")
	}
	funcs := template.FuncMap{
		"reais":    formatReais,
		"join":     strings.Join,
		"add":      func(a, b int) int { return a + b },
		"imageURL": func(key string) string { return imageBaseURL + "/" + key },
	}

	pages, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("couldn't list templates: %w", err)
	}

	t := &Templates{pages: map[string]*template.Template{}}
	for _, page := range pages {
		if page == "templates/base.html" {
			continue
		}
		tmpl, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/base.html", page)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse %s: %w", page, err)
		}
		t.pages[page[len("templates/"):]] = tmpl
	}
	return t, nil
}

func MustNewTemplates(imageBaseURL string) *Templates {
	t, err := NewTemplates(imageBaseURL)
	if err != nil {
		panic(fmt.Errorf("couldn't load templates: %w", err))
	}
	return t
}

// render writes page, inside the layout, with status.
func (t *Templates) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	t.renderTemplate(w, r, status, page, "base", data)
}

// renderFragment writes one template defined in page, without the
// layout: the piece of HTML an HTMX request swaps in.
func (t *Templates) renderFragment(w http.ResponseWriter, r *http.Request, status int, page, name string, data any) {
	t.renderTemplate(w, r, status, page, name, data)
}

// renderTemplate executes into a buffer first, so a template error becomes
// a clean 500 instead of half a page.
func (t *Templates) renderTemplate(w http.ResponseWriter, r *http.Request, status int, page, name string, data any) {
	tmpl, ok := t.pages[page]
	if !ok {
		slog.ErrorContext(r.Context(), "unknown template", "page", page)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		slog.ErrorContext(r.Context(), "couldn't render template", "page", page, "template", name, "error", err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "couldn't write response", "error", err)
	}
}
