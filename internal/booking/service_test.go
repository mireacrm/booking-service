package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/mireacrm/contracts-go/mirea/events/v1"
	"github.com/mireacrm/go-common/infra"
)

// fakeDirectory подменяет core и catalog: их gRPC-контракты уже описаны,
// а сами сервисы могут быть не написаны.
type fakeDirectory struct {
	shifts   []Interval
	duration time.Duration
	price    int64
	err      error
}

func (f fakeDirectory) EmployeeShifts(context.Context, uuid.UUID, Interval) ([]Interval, error) {
	return f.shifts, f.err
}

func (f fakeDirectory) Service(context.Context, uuid.UUID, uuid.UUID) (ServiceInfo, error) {
	if f.err != nil {
		return ServiceInfo{}, f.err
	}
	return ServiceInfo{ID: uuid.New(), Name: "Окрашивание", Duration: f.duration, PriceKopecks: f.price}, nil
}

type fakeRepo struct {
	created   []*Appointment
	taken     []Interval
	createErr error
}

func (f *fakeRepo) Create(_ context.Context, appointment *Appointment) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, appointment)
	return nil
}

func (f *fakeRepo) Get(context.Context, uuid.UUID) (*Appointment, error) { return nil, nil }

func (f *fakeRepo) TakenIntervals(context.Context, uuid.UUID, Interval) ([]Interval, error) {
	return f.taken, nil
}

func (f *fakeRepo) SetStatus(context.Context, uuid.UUID, Status, Status) (*Appointment, error) {
	return nil, nil
}

func (f *fakeRepo) ListByClient(
	context.Context, uuid.UUID, int, string,
) ([]*Appointment, string, error) {
	return f.created, "", nil
}

type fakeEvents struct{ published []string }

func (f *fakeEvents) Publish(_ context.Context, routingKey string, _ *eventsv1.EventEnvelope) error {
	f.published = append(f.published, routingKey)
	return nil
}

type fakeRealtime struct{ subjects []string }

func (f *fakeRealtime) Publish(_ context.Context, subject string, _ proto.Message) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

func newService(repo Repository, directory Directory) (*Service, *fakeEvents, *fakeRealtime) {
	events, realtime := &fakeEvents{}, &fakeRealtime{}
	return New(repo, directory, events, realtime), events, realtime
}

func request() BookRequest {
	return BookRequest{
		BranchID: uuid.New(), ClientID: uuid.New(),
		EmployeeID: uuid.New(), ServiceID: uuid.New(),
		StartsAt: at(10, 0),
	}
}

func TestBookSucceeds(t *testing.T) {
	repo := &fakeRepo{}
	service, events, realtime := newService(repo, fakeDirectory{shifts: []Interval{iv(9, 0, 18, 0)}, duration: time.Hour, price: 250000})

	appointment, err := service.Book(context.Background(), request())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if appointment.Status != StatusScheduled {
		t.Fatalf("статус %q", appointment.Status)
	}
	if appointment.PriceKopecks != 250000 {
		t.Fatalf("цена не зафиксирована: %d", appointment.PriceKopecks)
	}
	if !appointment.EndsAt.Equal(at(11, 0)) {
		t.Fatalf("конец рассчитан неверно: %v", appointment.EndsAt)
	}
	if len(events.published) != 1 || events.published[0] != "appointment.created" {
		t.Fatalf("события: %v", events.published)
	}
	if len(realtime.subjects) != 1 {
		t.Fatalf("табло не обновлено: %v", realtime.subjects)
	}
}

func TestBookRejectedOutsideShift(t *testing.T) {
	service, _, _ := newService(&fakeRepo{}, fakeDirectory{shifts: []Interval{iv(14, 0, 18, 0)}, duration: time.Hour})

	_, err := service.Book(context.Background(), request())

	var conflict *infra.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ожидался конфликт, получено %v", err)
	}
}

func TestBookRaceLostBecomesConflict(t *testing.T) {
	// Слот перехватили между проверкой и вставкой — ограничение БД вернуло
	// ErrSlotTaken, наружу это должно уйти конфликтом, а не пятисоткой.
	repo := &fakeRepo{createErr: ErrSlotTaken}
	service, events, _ := newService(repo, fakeDirectory{shifts: []Interval{iv(9, 0, 18, 0)}, duration: time.Hour})

	_, err := service.Book(context.Background(), request())

	var conflict *infra.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ожидался конфликт, получено %v", err)
	}
	if len(events.published) != 0 {
		t.Fatalf("при проигранной гонке событий быть не должно: %v", events.published)
	}
}

func TestFreeSlotsExcludesTaken(t *testing.T) {
	repo := &fakeRepo{taken: []Interval{iv(10, 0, 11, 0)}}
	service, _, _ := newService(repo, fakeDirectory{shifts: []Interval{iv(9, 0, 12, 0)}, duration: time.Hour})

	slots, err := service.FreeSlots(context.Background(), uuid.New(), uuid.New(), uuid.New(), iv(9, 0, 12, 0))
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	for _, slot := range slots {
		if (Interval{Start: slot.StartsAt, End: slot.EndsAt}).Overlaps(iv(10, 0, 11, 0)) {
			t.Fatalf("занятый слот попал в свободные: %v", slot.StartsAt.Format("15:04"))
		}
	}
	if len(slots) == 0 {
		t.Fatal("свободных слотов не осталось вовсе")
	}
}

func TestFreeSlotsRejectsBadWindow(t *testing.T) {
	service, _, _ := newService(&fakeRepo{}, fakeDirectory{duration: time.Hour})

	_, err := service.FreeSlots(context.Background(), uuid.New(), uuid.New(), uuid.New(), iv(12, 0, 9, 0))

	var invalid *infra.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("ожидался InvalidArgument, получено %v", err)
	}
}
