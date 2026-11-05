package device

import (
	"context"

	"github.com/google/uuid"
)

// Repository persists devices. Implementations must make the guard/mutate
// callbacks and the resulting write atomic: the callback runs while the row is
// locked, and the returned device is only exposed after a successful commit.
type Repository interface {
	Create(ctx context.Context, device Device) error
	Get(ctx context.Context, id uuid.UUID) (Device, error)
	List(ctx context.Context, filter Filter) ([]Device, error)
	Update(ctx context.Context, id uuid.UUID, mutate func(*Device) error) (Device, error)
	Delete(ctx context.Context, id uuid.UUID, guard func(Device) error) error
}
