//go:build integration

package booking

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mireacrm/booking-service/migrations"
	"github.com/mireacrm/go-common/infra"
)

// Интеграционные тесты вынесены под тег `integration`: `go test ./...` без базы
// остаётся быстрым, а в CI база поднимается отдельным шагом.
//
//	go test -tags=integration ./...

const defaultDSN = "postgres://booking_user:booking_pass@localhost:5432/booking_db_test"

func testDSN() string {
	if dsn := os.Getenv("BOOKING_TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	return defaultDSN
}

// newPool накатывает миграции и отдаёт чистую базу. Схема поднимается
// миграциями, а не CREATE TABLE в тесте: так проверяются и они.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	if err := infra.Migrate(ctx, testDSN(), migrations.Files); err != nil {
		t.Fatalf("миграции: %v", err)
	}

	pool, err := infra.NewPool(ctx, testDSN())
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, "TRUNCATE appointments CASCADE"); err != nil {
		t.Fatalf("очистка: %v", err)
	}
	return pool
}
