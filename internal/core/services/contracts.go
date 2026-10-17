package services

import "context"

type HealthService interface {
	// Ready reports whether the dependencies needed to serve are reachable.
	Ready(ctx context.Context) error
}
