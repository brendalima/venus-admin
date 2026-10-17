package services

import (
	"context"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

type HealthService interface {
	// Ready reports whether the dependencies needed to serve are reachable.
	Ready(ctx context.Context) error
}

type UserService interface {
	// Create adds a person to the admin. It is only reachable from
	// cmd/adduser: there is no public signup.
	Create(ctx context.Context, email, password string) (domain.User, error)

	// Authenticate returns the user when email and password match, and
	// *domain.UnauthorizedError when they do not, without saying which
	// of the two was wrong.
	Authenticate(ctx context.Context, email, password string) (domain.User, error)

	GetByID(ctx context.Context, id string) (domain.User, error)
}
