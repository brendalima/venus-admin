package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/brendalima/venus-admin/internal/core/domain"
	"github.com/brendalima/venus-admin/internal/repository"
	"github.com/brendalima/venus-admin/internal/storage"
)

var _ SiteService = (*Site)(nil)

type Site struct {
	repo       repository.SiteRepository
	publisher  storage.SitePublisher
	canPublish bool
}

type SiteInput struct {
	Repo      repository.SiteRepository
	Publisher storage.SitePublisher
	// CanPublish is false when publishing would reach no site, so the
	// admin does not offer it: in production, until the deploy hook is
	// configured.
	CanPublish bool
}

func NewSite(in SiteInput) (*Site, error) {
	switch {
	case in.Repo == nil:
		return nil, errors.New("missing site repository dependency")
	case in.Publisher == nil:
		return nil, errors.New("missing site publisher dependency")
	}
	return &Site{repo: in.Repo, publisher: in.Publisher, canPublish: in.CanPublish}, nil
}

func MustNewSite(in SiteInput) *Site {
	s, err := NewSite(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create site service: %w", err))
	}
	return s
}

func (s *Site) Status(ctx context.Context) (domain.SiteStatus, error) {
	pending, err := s.repo.HasUnpublishedChanges(ctx)
	if err != nil {
		return domain.SiteStatus{}, fmt.Errorf("couldn't check for unpublished changes: %w", err)
	}
	return domain.SiteStatus{Pending: pending, CanPublish: s.canPublish}, nil
}

// Publish calls the hook before recording anything. The other order could
// mark as published a catalogue the site never got. This way the worst
// case, a hook that worked and a record that failed, leaves the badge on
// and a second build on the next try, which is harmless.
//
// A change saved while this runs still reaches the site: the build reads
// the catalogue only after the hook returns.
func (s *Site) Publish(ctx context.Context) error {
	if !s.canPublish {
		return errors.New("publishing is not configured")
	}
	if err := s.publisher.Publish(ctx); err != nil {
		return fmt.Errorf("couldn't start site build: %w", err)
	}
	if err := s.repo.MarkPublished(ctx); err != nil {
		return fmt.Errorf("couldn't record publish: %w", err)
	}
	return nil
}
