package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeployHookPublish(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "build started", status: http.StatusOK},
		{name: "hook deleted in cloudflare", status: http.StatusNotFound, wantErr: true},
		{name: "cloudflare failing", status: http.StatusBadGateway, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			err := MustNewDeployHook(server.URL+"/hook/secret-id", nil).Publish(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
			if method != http.MethodPost {
				t.Errorf("method = %q, want POST", method)
			}
		})
	}
}

func TestDeployHookErrorHidesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	hook := MustNewDeployHook(server.URL+"/hook/secret-id", &http.Client{Timeout: 10 * time.Millisecond})
	err := hook.Publish(context.Background())
	if err == nil {
		t.Fatal("a timeout was not reported")
	}
	if strings.Contains(err.Error(), "secret-id") {
		t.Errorf("error leaks the hook URL: %v", err)
	}
}

func TestNewDeployHookRequiresURL(t *testing.T) {
	if _, err := NewDeployHook("", nil); err == nil {
		t.Error("an empty URL was accepted")
	}
}
