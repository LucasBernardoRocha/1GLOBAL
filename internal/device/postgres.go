package device

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository is the only production repository. Keeping it behind the
// small Repository interface makes the service easy to exercise in unit tests.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, d Device) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO devices (id, name, brand, state, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		d.ID, d.Name, d.Brand, string(d.State), d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (Device, error) {
	d, err := scanDevice(r.pool.QueryRow(ctx, `
		SELECT id, name, brand, state, created_at
		FROM devices
		WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("get device: %w", err)
	}
	return d, nil
}

func (r *PostgresRepository) List(ctx context.Context, filter Filter) ([]Device, error) {
	query := `SELECT id, name, brand, state, created_at FROM devices`
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)

	if filter.Brand != "" {
		args = append(args, filter.Brand)
		conditions = append(conditions, fmt.Sprintf("brand = $%d", len(args)))
	}
	if filter.State != "" {
		args = append(args, string(filter.State))
		conditions = append(conditions, fmt.Sprintf("state = $%d", len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	// Stable ordering keeps pagination predictable if it is added later.
	query += " ORDER BY created_at DESC, id DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]Device, 0)
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}
	return devices, nil
}

func (r *PostgresRepository) Update(ctx context.Context, id uuid.UUID, mutate func(*Device) error) (Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Device{}, fmt.Errorf("begin update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := lockDevice(ctx, tx, id)
	if err != nil {
		return Device{}, err
	}
	if err := mutate(&d); err != nil {
		return Device{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE devices
		SET name = $2, brand = $3, state = $4
		WHERE id = $1`,
		d.ID, d.Name, d.Brand, string(d.State),
	)
	if err != nil {
		return Device{}, fmt.Errorf("update device: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Device{}, fmt.Errorf("commit update: %w", err)
	}
	return d, nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id uuid.UUID, guard func(Device) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := lockDevice(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := guard(d); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

func lockDevice(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Device, error) {
	d, err := scanDevice(tx.QueryRow(ctx, `
		SELECT id, name, brand, state, created_at
		FROM devices
		WHERE id = $1
		FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("lock device: %w", err)
	}
	return d, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(row rowScanner) (Device, error) {
	var (
		d     Device
		state string
	)
	if err := row.Scan(&d.ID, &d.Name, &d.Brand, &state, &d.CreatedAt); err != nil {
		return Device{}, err
	}
	d.State = State(state)
	return d, nil
}
