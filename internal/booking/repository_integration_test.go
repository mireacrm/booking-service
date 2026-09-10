//go:build integration

package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mireacrm/go-common/infra"
)

func appointment(employee uuid.UUID, from, to time.Time, status Status) *Appointment {
	return &Appointment{
		ID:           uuid.New(),
		BranchID:     uuid.New(),
		ClientID:     uuid.New(),
		EmployeeID:   employee,
		ServiceID:    uuid.New(),
		StartsAt:     from,
		EndsAt:       to,
		Status:       status,
		PriceKopecks: 520000,
	}
}

func TestCreateAndGet(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	item := appointment(uuid.New(), at(9, 0), at(10, 0), StatusScheduled)

	if err := repo.Create(ctx, item); err != nil {
		t.Fatalf("создание: %v", err)
	}
	if item.CreatedAt.IsZero() {
		t.Fatal("created_at не заполнен базой")
	}

	found, err := repo.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if found.PriceKopecks != 520000 || found.Status != StatusScheduled {
		t.Fatalf("прочитано не то: %+v", found)
	}
}

func TestGetUnknownReturnsNotFound(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()

	_, err := repo.Get(ctx, uuid.New())

	var notFound *infra.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ожидался NotFound, получено %v", err)
	}
}

// Ключевой тест: ограничение EXCLUDE в базе, а не проверка в коде, решает
// гонку двух администраторов за один слот.
func TestOverlappingAppointmentRejectedByDatabase(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	employee := uuid.New()

	if err := repo.Create(ctx, appointment(employee, at(9, 0), at(10, 30), StatusScheduled)); err != nil {
		t.Fatalf("первая запись: %v", err)
	}

	err := repo.Create(ctx, appointment(employee, at(10, 0), at(11, 0), StatusScheduled))

	if !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("ожидался ErrSlotTaken, получено %v", err)
	}
}

func TestTouchingAppointmentsAllowed(t *testing.T) {
	// 10:00-10:00 — не пересечение: tstzrange полуоткрытый.
	repo, ctx := NewRepository(newPool(t)), context.Background()
	employee := uuid.New()

	if err := repo.Create(ctx, appointment(employee, at(9, 0), at(10, 0), StatusScheduled)); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	if err := repo.Create(ctx, appointment(employee, at(10, 0), at(11, 0), StatusScheduled)); err != nil {
		t.Fatalf("встык должно проходить: %v", err)
	}
}

func TestOtherEmployeeSameTimeAllowed(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()

	if err := repo.Create(ctx, appointment(uuid.New(), at(9, 0), at(10, 0), StatusScheduled)); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	if err := repo.Create(ctx, appointment(uuid.New(), at(9, 0), at(10, 0), StatusScheduled)); err != nil {
		t.Fatalf("разные специалисты не должны конфликтовать: %v", err)
	}
}

// Условие WHERE status IN ('scheduled','completed') в ограничении: отменённая
// запись слот освобождает. Опечатка здесь навсегда заблокировала бы время.
func TestCancelledAppointmentFreesSlot(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	employee := uuid.New()

	first := appointment(employee, at(9, 0), at(10, 0), StatusScheduled)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	if _, err := repo.SetStatus(ctx, first.ID, StatusScheduled, StatusCancelled); err != nil {
		t.Fatalf("отмена: %v", err)
	}

	if err := repo.Create(ctx, appointment(employee, at(9, 0), at(10, 0), StatusScheduled)); err != nil {
		t.Fatalf("слот должен освободиться после отмены: %v", err)
	}
}

func TestCompletedAppointmentKeepsSlot(t *testing.T) {
	// Завершённая запись слот не освобождает: визит состоялся.
	repo, ctx := NewRepository(newPool(t)), context.Background()
	employee := uuid.New()

	first := appointment(employee, at(9, 0), at(10, 0), StatusScheduled)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	if _, err := repo.SetStatus(ctx, first.ID, StatusScheduled, StatusCompleted); err != nil {
		t.Fatalf("завершение: %v", err)
	}

	err := repo.Create(ctx, appointment(employee, at(9, 0), at(10, 0), StatusScheduled))
	if !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("завершённая запись должна держать слот, получено %v", err)
	}
}

func TestSetStatusDistinguishesMissingFromWrongState(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	item := appointment(uuid.New(), at(9, 0), at(10, 0), StatusScheduled)
	if err := repo.Create(ctx, item); err != nil {
		t.Fatalf("создание: %v", err)
	}
	if _, err := repo.SetStatus(ctx, item.ID, StatusScheduled, StatusCompleted); err != nil {
		t.Fatalf("завершение: %v", err)
	}

	// Повторное завершение — конфликт, а не 404: запись существует.
	_, err := repo.SetStatus(ctx, item.ID, StatusScheduled, StatusCompleted)
	var conflict *infra.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ожидался конфликт, получено %v", err)
	}

	_, err = repo.SetStatus(ctx, uuid.New(), StatusScheduled, StatusCompleted)
	var notFound *infra.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ожидался NotFound, получено %v", err)
	}
}

func TestTakenIntervalsIgnoresCancelledAndOutOfWindow(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	employee := uuid.New()

	inside := appointment(employee, at(9, 0), at(10, 0), StatusScheduled)
	cancelled := appointment(employee, at(11, 0), at(12, 0), StatusScheduled)
	outside := appointment(employee, at(20, 0), at(21, 0), StatusScheduled)
	for _, item := range []*Appointment{inside, cancelled, outside} {
		if err := repo.Create(ctx, item); err != nil {
			t.Fatalf("создание: %v", err)
		}
	}
	if _, err := repo.SetStatus(ctx, cancelled.ID, StatusScheduled, StatusCancelled); err != nil {
		t.Fatalf("отмена: %v", err)
	}

	intervals, err := repo.TakenIntervals(ctx, employee, iv(8, 0, 13, 0))
	if err != nil {
		t.Fatalf("занятые интервалы: %v", err)
	}

	if len(intervals) != 1 || !intervals[0].Start.Equal(at(9, 0)) {
		t.Fatalf("ожидался один интервал 9:00, получено %+v", intervals)
	}
}

// Keyset-пагинация по (starts_at DESC, id): кортежное сравнение легко
// сломать на границе, и фейком это не проверить.
func TestListByClientPaginates(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	client := uuid.New()

	for hour := 9; hour < 14; hour++ {
		item := appointment(uuid.New(), at(hour, 0), at(hour, 30), StatusScheduled)
		item.ClientID = client
		if err := repo.Create(ctx, item); err != nil {
			t.Fatalf("создание: %v", err)
		}
	}

	var collected []*Appointment
	cursor := ""
	for page := 0; page < 10; page++ {
		items, next, err := repo.ListByClient(ctx, client, 2, cursor)
		if err != nil {
			t.Fatalf("страница %d: %v", page, err)
		}
		collected = append(collected, items...)
		if next == "" {
			break
		}
		cursor = next
	}

	if len(collected) != 5 {
		t.Fatalf("собрано %d записей вместо 5", len(collected))
	}
	for i := 1; i < len(collected); i++ {
		if !collected[i-1].StartsAt.After(collected[i].StartsAt) {
			t.Fatalf("нарушен порядок по убыванию: %v перед %v",
				collected[i-1].StartsAt, collected[i].StartsAt)
		}
	}

	seen := map[uuid.UUID]bool{}
	for _, item := range collected {
		if seen[item.ID] {
			t.Fatal("пагинация вернула дубль")
		}
		seen[item.ID] = true
	}
}

func TestListByClientIgnoresOthers(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()
	client := uuid.New()

	mine := appointment(uuid.New(), at(9, 0), at(10, 0), StatusScheduled)
	mine.ClientID = client
	if err := repo.Create(ctx, mine); err != nil {
		t.Fatalf("создание: %v", err)
	}
	if err := repo.Create(ctx, appointment(uuid.New(), at(11, 0), at(12, 0), StatusScheduled)); err != nil {
		t.Fatalf("создание чужой: %v", err)
	}

	items, _, err := repo.ListByClient(ctx, client, 10, "")
	if err != nil {
		t.Fatalf("список: %v", err)
	}
	if len(items) != 1 || items[0].ID != mine.ID {
		t.Fatalf("в выдачу попали чужие записи: %d", len(items))
	}
}

func TestListByClientRejectsBrokenCursor(t *testing.T) {
	repo, ctx := NewRepository(newPool(t)), context.Background()

	_, _, err := repo.ListByClient(ctx, uuid.New(), 10, "не-курсор")

	var invalid *infra.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("ожидался InvalidArgument, получено %v", err)
	}
}
