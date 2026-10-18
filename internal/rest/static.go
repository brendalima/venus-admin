package rest

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// Static files are embedded like the templates. Each name carries its
// version (htmx-2.0.4.min.js), so a file never changes under the same URL
// and browsers can keep it for good.
//
//go:embed static
var staticFS embed.FS

// NewStatic serves the embedded files. Mount it under /static/ with the
// prefix stripped.
func NewStatic() (http.Handler, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("couldn't open static files: %w", err)
	}
	files := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Checked here so a missing file, or a directory listing, is not
		// cached for a year along with the real ones.
		info, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	}), nil
}

func MustNewStatic() http.Handler {
	h, err := NewStatic()
	if err != nil {
		panic(fmt.Errorf("couldn't create static handler: %w", err))
	}
	return h
}

// NewMedia serves photos kept on local disk, standing in for the bucket in
// development. Mount it under /media/ with the prefix stripped. Like the
// bucket, it never lists directories.
func NewMedia(dir string) (http.Handler, error) {
	if dir == "" {
		return nil, errors.New("missing media directory")
	}
	root := os.DirFS(dir)
	files := http.FileServerFS(root)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := fs.Stat(root, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}), nil
}

func MustNewMedia(dir string) http.Handler {
	h, err := NewMedia(dir)
	if err != nil {
		panic(fmt.Errorf("couldn't create media handler: %w", err))
	}
	return h
}
