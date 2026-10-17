package storage

import (
	"context"
	"log/slog"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ Mailer = (*LogMailer)(nil)

// LogMailer writes each email to the log instead of sending it. It is for
// development only: a password reset link is printed in full, so anyone
// who can read the log can use it.
type LogMailer struct{}

func (LogMailer) Send(ctx context.Context, e domain.Email) error {
	slog.InfoContext(ctx, "email not sent, development mailer", "to", e.To, "subject", e.Subject, "text", e.Text)
	return nil
}
