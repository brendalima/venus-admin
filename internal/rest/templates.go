package rest

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
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

func NewTemplates() (*Templates, error) {
	pages, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("couldn't list templates: %w", err)
	}

	t := &Templates{pages: map[string]*template.Template{}}
	for _, page := range pages {
		if page == "templates/base.html" {
			continue
		}
		tmpl, err := template.ParseFS(templatesFS, "templates/base.html", page)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse %s: %w", page, err)
		}
		t.pages[page[len("templates/"):]] = tmpl
	}
	return t, nil
}

func MustNewTemplates() *Templates {
	t, err := NewTemplates()
	if err != nil {
		panic(fmt.Errorf("couldn't load templates: %w", err))
	}
	return t
}

// render writes page with status. It executes into a buffer first, so a
// template error becomes a clean 500 instead of half a page.
func (t *Templates) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	tmpl, ok := t.pages[page]
	if !ok {
		slog.ErrorContext(r.Context(), "unknown template", "page", page)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		slog.ErrorContext(r.Context(), "couldn't render template", "page", page, "error", err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "couldn't write response", "error", err)
	}
}
