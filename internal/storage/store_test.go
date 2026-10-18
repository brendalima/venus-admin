package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewR2RejectsMissingConfig(t *testing.T) {
	full := R2Input{AccountID: "acc", AccessKeyID: "id", SecretAccessKey: "secret", Bucket: "fotos"}
	for name, in := range map[string]R2Input{
		"account": {AccessKeyID: "id", SecretAccessKey: "secret", Bucket: "fotos"},
		"key id":  {AccountID: "acc", SecretAccessKey: "secret", Bucket: "fotos"},
		"secret":  {AccountID: "acc", AccessKeyID: "id", Bucket: "fotos"},
		"bucket":  {AccountID: "acc", AccessKeyID: "id", SecretAccessKey: "secret"},
	} {
		if _, err := NewR2(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewR2(full); err != nil {
		t.Errorf("complete input rejected: %v", err)
	}
}

func TestR2(t *testing.T) {
	type call struct {
		method, path, contentType, cacheControl, auth, body string
		checksumHeaders                                     []string
	}
	var calls []call
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{
			method: r.Method, path: r.URL.Path, body: string(body),
			contentType: r.Header.Get("Content-Type"), cacheControl: r.Header.Get("Cache-Control"),
			auth: r.Header.Get("Authorization"),
		}
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-amz-checksum-") {
				c.checksumHeaders = append(c.checksumHeaders, k)
			}
		}
		calls = append(calls, c)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	r2 := MustNewR2(R2Input{
		AccessKeyID: "id", SecretAccessKey: "secret", Bucket: "fotos",
		Endpoint: srv.URL, HTTPClient: srv.Client(),
	})
	ctx := context.Background()

	if err := r2.Put(ctx, "produtos/abc-600.jpg", []byte("jpeg bytes"), "image/jpeg"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := r2.Delete(ctx, "produtos/abc-600.jpg"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("%d requests, want 2", len(calls))
	}

	put, del := calls[0], calls[1]
	if put.method != http.MethodPut || put.path != "/fotos/produtos/abc-600.jpg" || put.body != "jpeg bytes" {
		t.Errorf("put sent %s %s with %q", put.method, put.path, put.body)
	}
	if put.contentType != "image/jpeg" || put.cacheControl != immutableCacheControl {
		t.Errorf("put headers: content type %q, cache control %q", put.contentType, put.cacheControl)
	}
	if !strings.HasPrefix(put.auth, "AWS4-HMAC-SHA256 Credential=id/") || !strings.Contains(put.auth, "/auto/s3/") {
		t.Errorf("put is not signed for R2: %q", put.auth)
	}
	if len(put.checksumHeaders) != 0 {
		t.Errorf("put sent checksum headers %v; R2 only needs them when required", put.checksumHeaders)
	}
	if del.method != http.MethodDelete || del.path != "/fotos/produtos/abc-600.jpg" {
		t.Errorf("delete sent %s %s", del.method, del.path)
	}

	status = http.StatusForbidden
	if err := r2.Put(ctx, "produtos/x.jpg", []byte("x"), "image/jpeg"); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("a refused put returned %v, want an error naming the cause", err)
	}
}

func TestDisk(t *testing.T) {
	dir := t.TempDir()
	d := MustNewDisk(dir)
	ctx := context.Background()

	if err := d.Put(ctx, "produtos/abc-600.jpg", []byte("jpeg bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "produtos", "abc-600.jpg"))
	if err != nil || string(got) != "jpeg bytes" {
		t.Fatalf("stored %q, %v", got, err)
	}

	if err := d.Delete(ctx, "produtos/abc-600.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "produtos", "abc-600.jpg")); !os.IsNotExist(err) {
		t.Error("the file is still there")
	}
	if err := d.Delete(ctx, "produtos/abc-600.jpg"); err != nil {
		t.Errorf("deleting a missing file: %v, want nil", err)
	}

	for _, key := range []string{"../escape.jpg", "/etc/passwd", "produtos/../../x"} {
		if err := d.Put(ctx, key, []byte("x"), "image/jpeg"); err == nil {
			t.Errorf("key %q escaped the directory", key)
		}
	}
}
