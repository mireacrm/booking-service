package booking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mireacrm/go-common/infra"
)

const overlapConstraint = "appointments_no_overlap"

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, appointment *Appointment) error {
	const query = `
		INSERT INTO appointments
			(id, branch_id, client_id, employee_id, service_id, starts_at, ends_at, status, price_kopecks)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at`

	err := r.pool.QueryRow(ctx, query,
		appointment.ID, appointment.BranchID, appointment.ClientID, appointment.EmployeeID,
		appointment.ServiceID, appointment.StartsAt, appointment.EndsAt,
		appointment.Status, appointment.PriceKopecks,
	).Scan(&appointment.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == overlapConstraint {
		return ErrSlotTaken
	}
	if err != nil {
		return fmt.Errorf("создание записи: %w", err)
	}
	return nil
}

func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	const query = `
		SELECT id, branch_id, client_id, employee_id, service_id,
		       starts_at, ends_at, status, price_kopecks, created_at
		FROM appointments WHERE id = $1`

	appointment, err := scanOne(r.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, infra.NotFound("appointment", id)
	}
	if err != nil {
		return nil, fmt.Errorf("чтение записи: %w", err)
	}
	return appointment, nil
}

func (r *PostgresRepository) TakenIntervals(
	ctx context.Context, employeeID uuid.UUID, window Interval,
) ([]Interval, error) {
	const query = `
		SELECT starts_at, ends_at FROM appointments
		WHERE employee_id = $1
		  AND status IN ('scheduled', 'completed')
		  AND tstzrange(starts_at, ends_at) && tstzrange($2, $3)
		ORDER BY starts_at`

	rows, err := r.pool.Query(ctx, query, employeeID, window.Start, window.End)
	if err != nil {
		return nil, fmt.Errorf("занятые интервалы: %w", err)
	}
	defer rows.Close()

	intervals := make([]Interval, 0)
	for rows.Next() {
		var item Interval
		if err := rows.Scan(&item.Start, &item.End); err != nil {
			return nil, err
		}
		intervals = append(intervals, item)
	}
	return intervals, rows.Err()
}

func (r *PostgresRepository) SetStatus(
	ctx context.Context, id uuid.UUID, from, to Status,
) (*Appointment, error) {
	const query = `
		UPDATE appointments SET status = $3
		WHERE id = $1 AND status = $2
		RETURNING id, branch_id, client_id, employee_id, service_id,
		          starts_at, ends_at, status, price_kopecks, created_at`

	appointment, err := scanOne(r.pool.QueryRow(ctx, query, id, from, to))
	if errors.Is(err, pgx.ErrNoRows) {
		// Записи либо нет вовсе, либо она уже не в исходном статусе —
		// различаем, чтобы не отдавать 404 на повторную отмену.
		if _, getErr := r.Get(ctx, id); getErr != nil {
			return nil, getErr
		}
		return nil, infra.Conflict("запись не в статусе %q", from)
	}
	if err != nil {
		return nil, fmt.Errorf("смена статуса: %w", err)
	}
	return appointment, nil
}

func scanOne(row pgx.Row) (*Appointment, error) {
	var item Appointment
	err := row.Scan(
		&item.ID, &item.BranchID, &item.ClientID, &item.EmployeeID, &item.ServiceID,
		&item.StartsAt, &item.EndsAt, &item.Status, &item.PriceKopecks, &item.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PostgresRepository) ListByClient(
	ctx context.Context, clientID uuid.UUID, limit int, cursor string,
) ([]*Appointment, string, error) {
	// Keyset по (starts_at DESC, id): свежие записи сверху, а смещение
	// пропускало бы строки, пока клиент листает и записывается.
	const query = `
		SELECT id, branch_id, client_id, employee_id, service_id,
		       starts_at, ends_at, status, price_kopecks, created_at
		FROM appointments
		WHERE client_id = $1
		  AND ($2::timestamptz IS NULL OR (starts_at, id) < ($2, $3))
		ORDER BY starts_at DESC, id DESC
		LIMIT $4`

	var cursorTime *time.Time
	cursorID := uuid.Nil
	if cursor != "" {
		raw, id, err := infra.DecodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, "", infra.InvalidArgument("некорректный курсор")
		}
		cursorTime, cursorID = &parsed, id
	}

	rows, err := r.pool.Query(ctx, query, clientID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("записи клиента: %w", err)
	}
	defer rows.Close()

	items := make([]*Appointment, 0, limit)
	for rows.Next() {
		item, err := scanRows(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	if len(items) > limit {
		last := items[limit-1]
		return items[:limit], infra.EncodeCursor(last.StartsAt.Format(time.RFC3339Nano), last.ID), nil
	}
	return items, "", nil
}

func scanRows(rows pgx.Rows) (*Appointment, error) {
	var item Appointment
	err := rows.Scan(
		&item.ID, &item.BranchID, &item.ClientID, &item.EmployeeID, &item.ServiceID,
		&item.StartsAt, &item.EndsAt, &item.Status, &item.PriceKopecks, &item.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}
