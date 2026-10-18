package rest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockSiteService struct {
	status     domain.SiteStatus
	statusErr  error
	publishErr error
	published  int
}

func (m *mockSiteService) Status(context.Context) (domain.SiteStatus, error) {
	return m.status, m.statusErr
}

func (m *mockSiteService) Publish(context.Context) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published++
	m.status.Pending = false
	return nil
}

func TestPublishBar(t *testing.T) {
	tests := []struct {
		name      string
		status    domain.SiteStatus
		statusErr error
		want      []string
		dontWant  []string
	}{
		{
			name:     "up to date",
			status:   domain.SiteStatus{CanPublish: true},
			want:     []string{"O site está em dia", `class="secondary">Publicar alterações`},
			dontWant: []string{"Há alterações não publicadas"},
		},
		{
			name:   "pending",
			status: domain.SiteStatus{Pending: true, CanPublish: true},
			want:   []string{"Há alterações não publicadas", "<button type=\"submit\">Publicar alterações"},
		},
		{
			// A button that publishes nowhere would mislead the owners.
			name:     "hook not configured",
			status:   domain.SiteStatus{Pending: true},
			want:     []string{"Há alterações não publicadas", "ainda não foi ligada"},
			dontWant: []string{"Publicar alterações", "/admin/publish"},
		},
		{
			name:      "status unavailable",
			statusErr: errors.New("database timeout"),
			want:      []string{"Não consegui conferir", "Publicar alterações"},
			dontWant:  []string{"database timeout"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.login(t)
			app.site.status, app.site.statusErr = tt.status, tt.statusErr

			res, body := app.get(t, "/admin", nil)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", res.StatusCode)
			}
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("list does not show %q", w)
				}
			}
			for _, w := range tt.dontWant {
				if strings.Contains(body, w) {
					t.Errorf("list shows %q", w)
				}
			}
		})
	}
}

func TestPublish(t *testing.T) {
	app := newTestApp(t, nil)

	if res, _ := app.post(t, "/admin/publish", nil); res.Header.Get("Location") != "/login" || app.site.published != 0 {
		t.Fatalf("anonymous publish: status %d, published %d", res.StatusCode, app.site.published)
	}

	app.login(t)
	app.site.status.Pending = true
	res, _ := app.post(t, "/admin/publish", nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" || app.site.published != 1 {
		t.Fatalf("publish: status %d, location %q, published %d", res.StatusCode, res.Header.Get("Location"), app.site.published)
	}
	_, body := app.get(t, "/admin", nil)
	if !strings.Contains(body, "Publicação iniciada — o site atualiza em cerca de 1 minuto.") {
		t.Error("the list does not confirm the publish")
	}

	app.site.publishErr = errors.New("deploy hook answered 502")
	app.post(t, "/admin/publish", nil)
	_, body = app.get(t, "/admin", nil)
	if !strings.Contains(body, "Não consegui publicar agora") || strings.Contains(body, "502") {
		t.Error("a failed publish is not reported, or leaks the cause")
	}
}
