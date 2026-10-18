package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

func TestNewResendRejectsMissingConfig(t *testing.T) {
	for name, in := range map[string]ResendInput{
		"api key": {From: "a@b.com"},
		"from":    {APIKey: "re_x"},
	} {
		if _, err := NewResend(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
}

func TestResendSend(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string // substring; empty means success
	}{
		{name: "sent", status: http.StatusOK, body: `{"id":"abc"}`},
		{
			name: "domain not verified", status: http.StatusForbidden,
			body:    `{"statusCode":403,"name":"validation_error","message":"The venusoasis.com.br domain is not verified."}`,
			wantErr: "403: validation_error: The venusoasis.com.br domain is not verified.",
		},
		{name: "not json", status: http.StatusBadGateway, body: "bad gateway", wantErr: "502: bad gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got resendEmail
			var auth, contentType, path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth, contentType, path = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decoding request: %v", err)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			m := MustNewResend(ResendInput{
				APIKey: "re_test", From: "Vênus Oasis <admin@venusoasis.com.br>",
				BaseURL: srv.URL, Client: srv.Client(),
			})
			err := m.Send(context.Background(), domain.Email{To: "ana@exemplo.com", Subject: "Oi", Text: "Olá"})

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if path != "/emails" || auth != "Bearer re_test" || contentType != "application/json" {
				t.Errorf("request: path %q, auth %q, content type %q", path, auth, contentType)
			}
			want := resendEmail{From: "Vênus Oasis <admin@venusoasis.com.br>", To: []string{"ana@exemplo.com"}, Subject: "Oi", Text: "Olá"}
			if got.From != want.From || len(got.To) != 1 || got.To[0] != want.To[0] || got.Subject != want.Subject || got.Text != want.Text {
				t.Errorf("sent %+v, want %+v", got, want)
			}
		})
	}
}
