-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TYPE appointment_status AS ENUM ('scheduled', 'completed', 'cancelled', 'no_show');

CREATE TABLE appointments (
    id            uuid PRIMARY KEY,
    branch_id     uuid        NOT NULL,
    client_id     uuid        NOT NULL,
    employee_id   uuid        NOT NULL,
    service_id    uuid        NOT NULL,
    starts_at     timestamptz NOT NULL,
    ends_at       timestamptz NOT NULL,
    status        appointment_status NOT NULL DEFAULT 'scheduled',
    price_kopecks bigint      NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT appointments_period_valid CHECK (ends_at > starts_at)
);

-- Гонку за слот разрешает база, а не проверка в коде: два администратора
-- могут жать «записать» одновременно. Отменённые записи слот освобождают,
-- поэтому ограничение действует только на активные.
ALTER TABLE appointments
    ADD CONSTRAINT appointments_no_overlap
    EXCLUDE USING gist (
        employee_id WITH =,
        tstzrange(starts_at, ends_at) WITH &&
    )
    WHERE (status IN ('scheduled', 'completed'));

CREATE INDEX appointments_employee_window ON appointments (employee_id, starts_at);
CREATE INDEX appointments_client ON appointments (client_id, starts_at DESC);

-- +goose Down
DROP TABLE appointments;
DROP TYPE appointment_status;
