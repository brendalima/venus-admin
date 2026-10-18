package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

var (
	_ SitePublisher = (*DeployHook)(nil)
	_ SitePublisher = LogPublisher{}
)

// DeployHook starts a build of the static site through a Cloudflare Pages
// deploy hook. It only asks for the build: whether the build then
// succeeds is not followed, and the pending-changes badge is what shows a
// build that did not pick the changes up.
type DeployHook struct {
	url    string
	client *http.Client
}

// NewDeployHook takes the hook's URL, which is a secret: anyone holding it
// can trigger builds. A nil client means one with a timeout.
func NewDeployHook(hookURL string, client *http.Client) (*DeployHook, error) {
	if hookURL == "" {
		return nil, errors.New("missing deploy hook url")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &DeployHook{url: hookURL, client: client}, nil
}

func MustNewDeployHook(hookURL string, client *http.Client) *DeployHook {
	h, err := NewDeployHook(hookURL, client)
	if err != nil {
		panic(fmt.Errorf("couldn't create deploy hook: %w", err))
	}
	return h
}

func (h *DeployHook) Publish(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, http.NoBody)
	if err != nil {
		// The URL is secret, and url.Error would repeat it.
		return errors.New("couldn't build deploy hook request")
	}
	res, err := h.client.Do(req)
	if err != nil {
		// *url.Error quotes the URL, which is secret and would reach the logs.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("couldn't call deploy hook: %w", err)
	}
	defer res.Body.Close()
	// Drained so the connection can be reused; what Cloudflare says on
	// success is not needed.
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("deploy hook answered %d", res.StatusCode)
	}
	return nil
}

// LogPublisher stands in for the deploy hook in development, where there
// is no site to build: it only logs.
type LogPublisher struct{}

func (LogPublisher) Publish(ctx context.Context) error {
	slog.InfoContext(ctx, "no deploy hook configured: the site would be rebuilt now")
	return nil
}
