package rest

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Static files are embedded like the templates. Each name carries its
// version (htmx-2.0.4.min.js), so a file never changes under the same URL
// and browsers can keep it for good.
//
//go:embed static
var staticFS embed.FS

// assetVersions holds a short hash of each embedded file, so a template
// can link to "/static/photos.js?v=…": the address changes whenever the
// file does, and browsers can still keep each version for good. Vendored
// files carry their version in the name; ours get it here.
var assetVersions = sync.OnceValue(func() map[string]string {
	versions := map[string]string{}
	_ = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		versions[strings.TrimPrefix(path, "static/")] = hex.EncodeToString(sum[:4])
		return nil
	})
	return versions
})

// assetURL is the address of an embedded static file, versioned by its
// content. An unknown name panics: it can only be a typo in a template,
// and templates are rendered in tests.
func assetURL(name string) string {
	v, ok := assetVersions()[name]
	if !ok {
		panic(fmt.Sprintf("unknown static file %q", name))
	}
	return "/static/" + name + "?v=" + v
}

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
