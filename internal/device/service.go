package device

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service contains the device use cases and the domain rules that span the
// repository and the notification side effect.
type Service struct {
	repo     Repository
	notifier Notifier
	now      func() time.Time
}

func NewService(repo Repository, notifier Notifier) *Service {
	return &Service{
		repo:     repo,
		notifier: notifier,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Device, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return Device{}, err
	}

	d := Device{
		ID:        uuid.New(),
		Name:      in.Name,
		Brand:     in.Brand,
		State:     in.State,
		CreatedAt: s.now(),
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return Device{}, err
	}
	return d, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Device, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, filter Filter) ([]Device, error) {
	filter.Brand = strings.TrimSpace(filter.Brand)
	if filter.State != "" && !filter.State.Valid() {
		return nil, &ValidationError{Field: "state", Message: "must be one of: available, in-use, inactive"}
	}
	return s.repo.List(ctx, filter)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (Device, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return Device{}, err
	}

	var previous State
	updated, err := s.repo.Update(ctx, id, func(d *Device) error {
		// The repository runs this callback while holding a row lock, so the
		// in-use check cannot race with a concurrent state change.
		previous = d.State
		if d.State == StateInUse && (in.Name != nil || in.Brand != nil) {
			return ErrInUse
		}
		if in.Name != nil {
			d.Name = *in.Name
		}
		if in.Brand != nil {
			d.Brand = *in.Brand
		}
		if in.State != nil {
			d.State = *in.State
		}
		return nil
	})
	if err != nil {
		return Device{}, err
	}

	// Publish only after the repository committed. The challenge does not
	// require replay, so an in-process broker is sufficient; a production
	// system should use a transactional outbox and a durable broker.
	if previous != updated.State {
		s.notifier.Publish(StateChangedEvent{
			DeviceID:      updated.ID,
			PreviousState: previous,
			NewState:      updated.State,
			ChangedAt:     s.now(),
		})
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, func(d Device) error {
		if d.State == StateInUse {
			return ErrInUse
		}
		return nil
	})
}
