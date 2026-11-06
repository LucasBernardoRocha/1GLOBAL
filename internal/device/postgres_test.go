package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"devices-api/internal/platform"
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(m.Run())
	}

	if err := platform.Migrate(context.Background(), dsn); err != nil {
		fmt.Fprintf(os.Stderr, "migrate test database: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func newPostgresRepository(t *testing.T) *PostgresRepository {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := platform.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `TRUNCATE devices`); err != nil {
		t.Fatalf("truncate devices: %v", err)
	}
	return NewPostgresRepository(pool)
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	if err := platform.Migrate(context.Background(), dsn); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
}

func TestPostgresCreateAndGet(t *testing.T) {
	repo := newPostgresRepository(t)
	ctx := context.Background()

	want := Device{
		ID:        uuid.New(),
		Name:      "iPhone 15",
		Brand:     "Apple",
		State:     StateAvailable,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.Brand != want.Brand || got.State != want.State {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("Get() created_at = %v, want %v", got.CreatedAt, want.CreatedAt)
	}

	if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestPostgresList(t *testing.T) {
	repo := newPostgresRepository(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	devices := []Device{
		{ID: uuid.New(), Name: "iPhone 15", Brand: "Apple", State: StateAvailable, CreatedAt: now.Add(-time.Hour)},
		{ID: uuid.New(), Name: "iPad Pro", Brand: "Apple", State: StateInUse, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: uuid.New(), Name: "Galaxy S24", Brand: "Samsung", State: StateAvailable, CreatedAt: now.Add(-3 * time.Hour)},
	}
	for _, d := range devices {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	tests := []struct {
		name   string
		filter Filter
		want   int
	}{
		{name: "all", filter: Filter{}, want: 3},
		{name: "by brand", filter: Filter{Brand: "Apple"}, want: 2},
		{name: "by state", filter: Filter{State: StateInUse}, want: 1},
		{name: "combined", filter: Filter{Brand: "Apple", State: StateAvailable}, want: 1},
		{name: "no match", filter: Filter{Brand: "Sony"}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(got) != tt.want {
				t.Fatalf("List() returned %d devices, want %d", len(got), tt.want)
			}
		})
	}
}

func TestPostgresUpdate(t *testing.T) {
	repo := newPostgresRepository(t)
	ctx := context.Background()

	original := Device{
		ID:        uuid.New(),
		Name:      "Old name",
		Brand:     "Apple",
		State:     StateAvailable,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.Create(ctx, original); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := repo.Update(ctx, original.ID, func(d *Device) error {
		d.Name = "New name"
		d.State = StateInUse
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "New name" || updated.State != StateInUse {
		t.Fatalf("Update() = %+v, want updated fields", updated)
	}

	stored, err := repo.Get(ctx, original.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.ID != updated.ID || stored.Name != updated.Name || stored.Brand != updated.Brand || stored.State != updated.State {
		t.Fatalf("stored device = %+v, want %+v", stored, updated)
	}
	if !stored.CreatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("stored created_at = %v, want %v", stored.CreatedAt, updated.CreatedAt)
	}

	rejected := errors.New("rejected by guard")
	if _, err := repo.Update(ctx, original.ID, func(*Device) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatalf("Update() error = %v, want %v", err, rejected)
	}
	stored, err = repo.Get(ctx, original.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Name != "New name" || stored.State != StateInUse {
		t.Fatalf("rejected update changed the device: %+v", stored)
	}

	if _, err := repo.Update(ctx, uuid.New(), func(*Device) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestPostgresDelete(t *testing.T) {
	repo := newPostgresRepository(t)
	ctx := context.Background()

	t.Run("deletes an available device", func(t *testing.T) {
		d := Device{
			ID: uuid.New(), Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if err := repo.Delete(ctx, d.ID, func(Device) error { return nil }); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := repo.Get(ctx, d.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("guard rejects an in-use device", func(t *testing.T) {
		d := Device{
			ID: uuid.New(), Name: "iPhone 15", Brand: "Apple",
			State: StateInUse, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		err := repo.Delete(ctx, d.ID, func(current Device) error {
			if current.State == StateInUse {
				return ErrInUse
			}
			return nil
		})
		if !errors.Is(err, ErrInUse) {
			t.Fatalf("Delete() error = %v, want ErrInUse", err)
		}
		if _, err := repo.Get(ctx, d.ID); err != nil {
			t.Fatalf("in-use device was removed: %v", err)
		}
	})

	t.Run("missing device", func(t *testing.T) {
		if err := repo.Delete(ctx, uuid.New(), func(Device) error { return nil }); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete() error = %v, want ErrNotFound", err)
		}
	})
}
