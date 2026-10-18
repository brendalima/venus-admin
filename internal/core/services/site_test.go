package services

import (
	"context"
	"errors"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type mockSiteRepository struct {
	pending   bool
	readErr   error
	markErr   error
	published int
}

func (m *mockSiteRepository) HasUnpublishedChanges(context.Context) (bool, error) {
	return m.pending, m.readErr
}

func (m *mockSiteRepository) MarkPublished(context.Context) error {
	if m.markErr != nil {
		return m.markErr
	}
	m.published++
	m.pending = false
	return nil
}

type mockPublisher struct {
	err   error
	calls int
}

func (m *mockPublisher) Publish(context.Context) error {
	m.calls++
	return m.err
}

func TestNewSite(t *testing.T) {
	for name, in := range map[string]SiteInput{
		"repository": {Publisher: &mockPublisher{}},
		"publisher":  {Repo: &mockSiteRepository{}},
	} {
		if _, err := NewSite(in); err == nil {
			t.Errorf("missing %s was accepted", name)
		}
	}
	if _, err := NewSite(SiteInput{Repo: &mockSiteRepository{}, Publisher: &mockPublisher{}}); err != nil {
		t.Errorf("complete input refused: %v", err)
	}
}

func TestSiteStatus(t *testing.T) {
	tests := []struct {
		name       string
		repo       *mockSiteRepository
		canPublish bool
		want       domain.SiteStatus
		wantErr    bool
	}{
		{name: "up to date", repo: &mockSiteRepository{}, canPublish: true, want: domain.SiteStatus{CanPublish: true}},
		{name: "pending", repo: &mockSiteRepository{pending: true}, canPublish: true, want: domain.SiteStatus{Pending: true, CanPublish: true}},
		{name: "no hook yet", repo: &mockSiteRepository{pending: true}, want: domain.SiteStatus{Pending: true}},
		{name: "database down", repo: &mockSiteRepository{readErr: errors.New("timeout")}, canPublish: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := MustNewSite(SiteInput{Repo: tt.repo, Publisher: &mockPublisher{}, CanPublish: tt.canPublish})
			got, err := s.Status(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Status() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Status() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSitePublish(t *testing.T) {
	tests := []struct {
		name          string
		canPublish    bool
		hookErr       error
		markErr       error
		wantErr       bool
		wantHookCalls int
		wantPublished int
	}{
		{name: "published", canPublish: true, wantHookCalls: 1, wantPublished: 1},
		{
			// Recording first would hide changes the site never got.
			name: "hook fails, nothing recorded", canPublish: true, hookErr: errors.New("502"),
			wantErr: true, wantHookCalls: 1,
		},
		{name: "record fails", canPublish: true, markErr: errors.New("timeout"), wantErr: true, wantHookCalls: 1},
		{name: "not configured", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockSiteRepository{pending: true, markErr: tt.markErr}
			hook := &mockPublisher{err: tt.hookErr}
			s := MustNewSite(SiteInput{Repo: repo, Publisher: hook, CanPublish: tt.canPublish})

			err := s.Publish(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
			if hook.calls != tt.wantHookCalls || repo.published != tt.wantPublished {
				t.Errorf("hook calls %d, published %d; want %d and %d",
					hook.calls, repo.published, tt.wantHookCalls, tt.wantPublished)
			}
		})
	}
}
