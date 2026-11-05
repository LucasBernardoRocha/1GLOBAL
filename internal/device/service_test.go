package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceCreate(t *testing.T) {
	service, repo, notifier := newTestService()

	created, err := service.Create(context.Background(), CreateInput{
		Name:  "  iPhone 15  ",
		Brand: " Apple ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if created.ID == uuid.Nil {
		t.Fatal("Create() did not generate an ID")
	}
	if created.Name != "iPhone 15" || created.Brand != "Apple" {
		t.Fatalf("Create() = %q/%q, want trimmed values", created.Name, created.Brand)
	}
	if created.State != StateAvailable {
		t.Fatalf("Create() state = %q, want %q", created.State, StateAvailable)
	}
	if !created.CreatedAt.Equal(testNow) {
		t.Fatalf("Create() created_at = %v, want %v", created.CreatedAt, testNow)
	}

	stored, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("stored device not found: %v", err)
	}
	if stored != created {
		t.Fatalf("stored device = %+v, want %+v", stored, created)
	}
	if events := notifier.Events(); len(events) != 0 {
		t.Fatalf("Create() published %d events, want 0", len(events))
	}
}

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		field string
	}{
		{name: "missing name", input: CreateInput{Brand: "Apple"}, field: "name"},
		{name: "missing brand", input: CreateInput{Name: "iPhone 15"}, field: "brand"},
		{name: "invalid state", input: CreateInput{Name: "iPhone 15", Brand: "Apple", State: "lost"}, field: "state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo, _ := newTestService()
			_, err := service.Create(context.Background(), tt.input)

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Create() error = %v, want *ValidationError", err)
			}
			if validationErr.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationErr.Field, tt.field)
			}

			devices, err := repo.List(context.Background(), Filter{})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(devices) != 0 {
				t.Fatalf("Create() persisted %d devices, want 0", len(devices))
			}
		})
	}
}

func TestServiceGet(t *testing.T) {
	id := uuid.New()
	want := Device{ID: id, Name: "iPhone 15", Brand: "Apple", State: StateAvailable, CreatedAt: testNow}
	service, _, _ := newTestService(want)

	got, err := service.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}

	_, err = service.Get(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestServiceList(t *testing.T) {
	appleAvailable := Device{
		ID: uuid.New(), Name: "iPhone 15", Brand: "Apple",
		State: StateAvailable, CreatedAt: testNow.Add(-time.Hour),
	}
	appleInUse := Device{
		ID: uuid.New(), Name: "iPad Pro", Brand: "Apple",
		State: StateInUse, CreatedAt: testNow.Add(-2 * time.Hour),
	}
	samsung := Device{
		ID: uuid.New(), Name: "Galaxy S24", Brand: "Samsung",
		State: StateAvailable, CreatedAt: testNow.Add(-3 * time.Hour),
	}

	tests := []struct {
		name    string
		filter  Filter
		want    int
		wantErr bool
	}{
		{name: "all", filter: Filter{}, want: 3},
		{name: "by brand", filter: Filter{Brand: "Apple"}, want: 2},
		{name: "by state", filter: Filter{State: StateInUse}, want: 1},
		{name: "by brand and state", filter: Filter{Brand: "Apple", State: StateAvailable}, want: 1},
		{name: "trims brand", filter: Filter{Brand: " Apple "}, want: 2},
		{name: "invalid state", filter: Filter{State: "lost"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _, _ := newTestService(appleAvailable, appleInUse, samsung)
			devices, err := service.List(context.Background(), tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("List() error = nil, want validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(devices) != tt.want {
				t.Fatalf("List() returned %d devices, want %d", len(devices), tt.want)
			}
		})
	}
}

func TestServiceListRepositoryError(t *testing.T) {
	service, repo, _ := newTestService()
	repo.listErr = errors.New("database unavailable")

	if _, err := service.List(context.Background(), Filter{}); err == nil {
		t.Fatal("List() error = nil, want repository error")
	}
}

func TestServiceUpdate(t *testing.T) {
	t.Run("updates mutable fields and publishes state change", func(t *testing.T) {
		id := uuid.New()
		service, repo, notifier := newTestService(Device{
			ID: id, Name: "Old name", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		name := "  New name  "
		state := StateInUse
		updated, err := service.Update(context.Background(), id, UpdateInput{
			Name:  &name,
			State: &state,
		})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Name != "New name" || updated.State != StateInUse {
			t.Fatalf("Update() = %+v, want trimmed name and in-use state", updated)
		}

		stored, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("stored device not found: %v", err)
		}
		if stored != updated {
			t.Fatalf("stored device = %+v, want %+v", stored, updated)
		}

		events := notifier.Events()
		if len(events) != 1 {
			t.Fatalf("Update() published %d events, want 1", len(events))
		}
		event := events[0]
		if event.DeviceID != id || event.PreviousState != StateAvailable || event.NewState != StateInUse {
			t.Fatalf("event = %+v, want previous=%s new=%s", event, StateAvailable, StateInUse)
		}
		if !event.ChangedAt.Equal(testNow) {
			t.Fatalf("event.ChangedAt = %v, want %v", event.ChangedAt, testNow)
		}
	})

	t.Run("same state does not publish", func(t *testing.T) {
		id := uuid.New()
		service, _, notifier := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		state := StateAvailable
		if _, err := service.Update(context.Background(), id, UpdateInput{State: &state}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if events := notifier.Events(); len(events) != 0 {
			t.Fatalf("Update() published %d events, want 0", len(events))
		}
	})

	t.Run("in-use device rejects name and brand updates", func(t *testing.T) {
		name := "New name"
		brand := "Samsung"

		tests := []struct {
			name  string
			input UpdateInput
		}{
			{name: "name", input: UpdateInput{Name: &name}},
			{name: "brand", input: UpdateInput{Brand: &brand}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				id := uuid.New()
				service, repo, notifier := newTestService(Device{
					ID: id, Name: "Old name", Brand: "Apple",
					State: StateInUse, CreatedAt: testNow,
				})

				_, err := service.Update(context.Background(), id, tt.input)
				if !errors.Is(err, ErrInUse) {
					t.Fatalf("Update() error = %v, want ErrInUse", err)
				}

				stored, err := repo.Get(context.Background(), id)
				if err != nil {
					t.Fatalf("stored device not found: %v", err)
				}
				if stored.Name != "Old name" || stored.Brand != "Apple" {
					t.Fatalf("device changed while in use: %+v", stored)
				}
				if events := notifier.Events(); len(events) != 0 {
					t.Fatalf("Update() published %d events, want 0", len(events))
				}
			})
		}
	})

	t.Run("in-use device can change state only", func(t *testing.T) {
		id := uuid.New()
		service, _, notifier := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateInUse, CreatedAt: testNow,
		})

		state := StateInactive
		updated, err := service.Update(context.Background(), id, UpdateInput{State: &state})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.State != StateInactive {
			t.Fatalf("Update() state = %q, want %q", updated.State, StateInactive)
		}

		events := notifier.Events()
		if len(events) != 1 || events[0].PreviousState != StateInUse || events[0].NewState != StateInactive {
			t.Fatalf("events = %+v, want in-use -> inactive", events)
		}
	})

	t.Run("created_at is immutable", func(t *testing.T) {
		id := uuid.New()
		service, repo, _ := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		createdAt := testNow.Add(time.Hour)
		_, err := service.Update(context.Background(), id, UpdateInput{CreatedAt: &createdAt})
		if !errors.Is(err, ErrImmutableCreatedAt) {
			t.Fatalf("Update() error = %v, want ErrImmutableCreatedAt", err)
		}

		stored, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("stored device not found: %v", err)
		}
		if !stored.CreatedAt.Equal(testNow) {
			t.Fatalf("created_at changed to %v, want %v", stored.CreatedAt, testNow)
		}
	})

	t.Run("validates input", func(t *testing.T) {
		id := uuid.New()
		service, _, _ := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		empty := " "
		_, err := service.Update(context.Background(), id, UpdateInput{Name: &empty})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("Update() error = %v, want *ValidationError", err)
		}
	})

	t.Run("repository failure does not publish", func(t *testing.T) {
		id := uuid.New()
		service, repo, notifier := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})
		repo.updateErr = errors.New("database unavailable")

		name := "New name"
		if _, err := service.Update(context.Background(), id, UpdateInput{Name: &name}); err == nil {
			t.Fatal("Update() error = nil, want repository error")
		}
		if events := notifier.Events(); len(events) != 0 {
			t.Fatalf("Update() published %d events after failure, want 0", len(events))
		}
	})

	t.Run("missing device", func(t *testing.T) {
		service, _, _ := newTestService()
		name := "New name"

		_, err := service.Update(context.Background(), uuid.New(), UpdateInput{Name: &name})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})
}

func TestServiceDelete(t *testing.T) {
	t.Run("deletes an available device", func(t *testing.T) {
		id := uuid.New()
		service, repo, _ := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		if err := service.Delete(context.Background(), id); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := repo.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("rejects an in-use device", func(t *testing.T) {
		id := uuid.New()
		service, repo, _ := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateInUse, CreatedAt: testNow,
		})

		if err := service.Delete(context.Background(), id); !errors.Is(err, ErrInUse) {
			t.Fatalf("Delete() error = %v, want ErrInUse", err)
		}
		if _, err := repo.Get(context.Background(), id); err != nil {
			t.Fatalf("in-use device was removed: %v", err)
		}
	})

	t.Run("missing device", func(t *testing.T) {
		service, _, _ := newTestService()
		if err := service.Delete(context.Background(), uuid.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("repository failure", func(t *testing.T) {
		id := uuid.New()
		service, repo, _ := newTestService(Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})
		repo.deleteErr = errors.New("database unavailable")

		if err := service.Delete(context.Background(), id); err == nil {
			t.Fatal("Delete() error = nil, want repository error")
		}
	})
}
