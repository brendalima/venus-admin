package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ Mailer = (*Resend)(nil)

const resendBaseURL = "https://api.resend.com"

// Resend sends email through Resend's HTTP API. It is one POST, so it
// talks to the API directly instead of pulling in the SDK.
type Resend struct {
	apiKey  string
	from    string
	baseURL string
	client  *http.Client
}

type ResendInput struct {
	APIKey string
	// From is the sender, such as "Vênus Oasis <admin@venusoasis.com.br>".
	// Its domain must be verified in Resend.
	From string

	// BaseURL and Client exist for tests; empty and nil mean Resend's API
	// and a client with a timeout.
	BaseURL string
	Client  *http.Client
}

func NewResend(in ResendInput) (*Resend, error) {
	switch {
	case in.APIKey == "":
		return nil, errors.New("missing resend api key")
	case in.From == "":
		return nil, errors.New("missing sender address")
	}
	r := &Resend{apiKey: in.APIKey, from: in.From, baseURL: in.BaseURL, client: in.Client}
	if r.baseURL == "" {
		r.baseURL = resendBaseURL
	}
	if r.client == nil {
		// Someone is waiting on the page while this runs.
		r.client = &http.Client{Timeout: 10 * time.Second}
	}
	return r, nil
}

func MustNewResend(in ResendInput) *Resend {
	r, err := NewResend(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create resend mailer: %w", err))
	}
	return r
}

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

type resendError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (r *Resend) Send(ctx context.Context, e domain.Email) error {
	body, err := json.Marshal(resendEmail{From: r.from, To: []string{e.To}, Subject: e.Subject, Text: e.Text})
	if err != nil {
		return fmt.Errorf("couldn't encode email: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("couldn't build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach resend: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode/100 == 2 {
		// Drained so the connection can be reused.
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}

	// Resend explains failures (an unverified domain, a revoked key) in
	// the body, which is what the log needs to make them fixable.
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	var re resendError
	if json.Unmarshal(raw, &re) == nil && re.Message != "" {
		return fmt.Errorf("resend answered %d: %s: %s", res.StatusCode, re.Name, re.Message)
	}
	return fmt.Errorf("resend answered %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
}
