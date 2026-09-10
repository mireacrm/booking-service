package booking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/mireacrm/contracts-go/mirea/common/v1"
	eventsv1 "github.com/mireacrm/contracts-go/mirea/events/v1"
	realtimev1 "github.com/mireacrm/contracts-go/mirea/realtime/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/mireacrm/booking-service/internal/access"
	"github.com/mireacrm/go-common/infra"
)

// Directory — то, что booking спрашивает у соседей. Реализуется gRPC-клиентами
// к core и catalog; в тестах подменяется фейком.
type Directory interface {
	EmployeeShifts(ctx context.Context, employeeID uuid.UUID, window Interval) ([]Interval, error)
	Service(ctx context.Context, serviceID, branchID uuid.UUID) (ServiceInfo, error)
}

type ServiceInfo struct {
	ID           uuid.UUID
	Name         string
	Duration     time.Duration
	PriceKopecks int64
}

// Repository — хранилище записей.
type Repository interface {
	Create(ctx context.Context, appointment *Appointment) error
	Get(ctx context.Context, id uuid.UUID) (*Appointment, error)
	TakenIntervals(ctx context.Context, employeeID uuid.UUID, window Interval) ([]Interval, error)
	SetStatus(ctx context.Context, id uuid.UUID, from, to Status) (*Appointment, error)
	ListByClient(ctx context.Context, clientID uuid.UUID, limit int, cursor string) ([]*Appointment, string, error)
}

// ErrSlotTaken возвращается репозиторием, когда слот перехватили между
// проверкой и вставкой. Отличается от обычного конфликта: это гонка.
var ErrSlotTaken = errors.New("слот занят")

// Events — доменные события, которые нельзя потерять (RabbitMQ).
type Events interface {
	Publish(ctx context.Context, routingKey string, envelope *eventsv1.EventEnvelope) error
}

// Realtime — эфемерные обновления табло (NATS).
type Realtime interface {
	Publish(ctx context.Context, subject string, message proto.Message) error
}

type Service struct {
	repo      Repository
	directory Directory
	events    Events
	realtime  Realtime
}

func New(repo Repository, directory Directory, events Events, realtime Realtime) *Service {
	return &Service{repo: repo, directory: directory, events: events, realtime: realtime}
}

func scheduleSubject(branchID uuid.UUID) string {
	return fmt.Sprintf("mirea.branch.%s.schedule", branchID)
}

// announce публикует после успешной записи в базу. Ошибка публикации не
// откатывает операцию: запись уже создана, а событие можно только залогировать.
// Это dual write; промышленное решение — транзакционный outbox.
func (s *Service) announce(
	ctx context.Context, routingKey string, envelope *eventsv1.EventEnvelope,
) {
	if err := s.events.Publish(ctx, routingKey, envelope); err != nil {
		slog.ErrorContext(ctx, "событие не опубликовано",
			"routing_key", routingKey, "error", err,
			"trace_id", infra.TraceID(infra.Traceparent(ctx)))
	}
}

func (s *Service) announceSlot(ctx context.Context, item *Appointment, taken bool) {
	message := &realtimev1.SlotChanged{
		BranchId:   item.BranchID.String(),
		EmployeeId: item.EmployeeID.String(),
		Period: &commonv1.TimeRange{
			StartAt: timestamppb.New(item.StartsAt),
			EndAt:   timestamppb.New(item.EndsAt),
		},
		IsTaken: taken,
	}
	if taken {
		message.AppointmentId = item.ID.String()
	}
	if err := s.realtime.Publish(ctx, scheduleSubject(item.BranchID), message); err != nil {
		slog.WarnContext(ctx, "табло не обновлено", "error", err)
	}
}

type BookRequest struct {
	BranchID   uuid.UUID
	ClientID   uuid.UUID
	EmployeeID uuid.UUID
	ServiceID  uuid.UUID
	StartsAt   time.Time
}

func (s *Service) Book(ctx context.Context, request BookRequest) (*Appointment, error) {
	service, err := s.directory.Service(ctx, request.ServiceID, request.BranchID)
	if err != nil {
		return nil, err
	}

	wanted := Interval{Start: request.StartsAt, End: request.StartsAt.Add(service.Duration)}
	if !wanted.Valid() {
		return nil, infra.InvalidArgument("длительность услуги должна быть положительной")
	}

	shifts, err := s.directory.EmployeeShifts(ctx, request.EmployeeID, wanted)
	if err != nil {
		return nil, err
	}
	if !FitsShift(shifts, wanted) {
		return nil, infra.Conflict("специалист не работает в это время")
	}

	appointment := &Appointment{
		ID:           uuid.New(),
		BranchID:     request.BranchID,
		ClientID:     request.ClientID,
		EmployeeID:   request.EmployeeID,
		ServiceID:    request.ServiceID,
		StartsAt:     wanted.Start,
		EndsAt:       wanted.End,
		Status:       StatusScheduled,
		PriceKopecks: service.PriceKopecks,
	}

	// Гонку разрешает ограничение в БД, а не эта проверка: между чтением
	// занятых интервалов и вставкой слот может перехватить другой запрос.
	if err := s.repo.Create(ctx, appointment); err != nil {
		if errors.Is(err, ErrSlotTaken) {
			return nil, infra.Conflict("слот уже занят")
		}
		return nil, err
	}

	s.announce(ctx, "appointment.created", &eventsv1.EventEnvelope{
		Payload: &eventsv1.EventEnvelope_AppointmentCreated{
			AppointmentCreated: &eventsv1.AppointmentCreated{
				AppointmentId: appointment.ID.String(),
				BranchId:      appointment.BranchID.String(),
				ClientId:      appointment.ClientID.String(),
				EmployeeId:    appointment.EmployeeID.String(),
				ServiceId:     appointment.ServiceID.String(),
				Period: &commonv1.TimeRange{
					StartAt: timestamppb.New(appointment.StartsAt),
					EndAt:   timestamppb.New(appointment.EndsAt),
				},
			},
		},
	})
	s.announceSlot(ctx, appointment, true)
	return appointment, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureOwn(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// ensureOwn — специалист работает только со своими визитами. Роль проверил
// шлюз, а кому принадлежит визит, знает только этот сервис.
func (s *Service) ensureOwn(ctx context.Context, item *Appointment) error {
	return access.EnsureOwner(infra.CallerFrom(ctx), item.EmployeeID.String(), "визит")
}

func (s *Service) ListByClient(
	ctx context.Context, clientID uuid.UUID, limit int, cursor string,
) ([]*Appointment, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListByClient(ctx, clientID, limit, cursor)
}

// Complete — ключевое событие системы: его ждут inventory (списать материалы),
// billing (выставить счёт) и analytics (посчитать выручку).
func (s *Service) Complete(ctx context.Context, id uuid.UUID) (*Appointment, error) {
	// Проверка до смены состояния: закрыть чужой визит нельзя даже на миг.
	// Лишнее чтение выполняется только для непривилегированного вызывающего.
	if caller := infra.CallerFrom(ctx); caller.Known() && !access.Privileged(caller) {
		existing, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := s.ensureOwn(ctx, existing); err != nil {
			return nil, err
		}
	}

	item, err := s.repo.SetStatus(ctx, id, StatusScheduled, StatusCompleted)
	if err != nil {
		return nil, err
	}

	s.announce(ctx, "appointment.completed", &eventsv1.EventEnvelope{
		Payload: &eventsv1.EventEnvelope_AppointmentCompleted{
			AppointmentCompleted: &eventsv1.AppointmentCompleted{
				AppointmentId: item.ID.String(),
				BranchId:      item.BranchID.String(),
				ClientId:      item.ClientID.String(),
				EmployeeId:    item.EmployeeID.String(),
				ServiceId:     item.ServiceID.String(),
			},
		},
	})
	return item, nil
}

func (s *Service) Cancel(ctx context.Context, id uuid.UUID, reason string) (*Appointment, error) {
	item, err := s.repo.SetStatus(ctx, id, StatusScheduled, StatusCancelled)
	if err != nil {
		return nil, err
	}

	s.announce(ctx, "appointment.cancelled", &eventsv1.EventEnvelope{
		Payload: &eventsv1.EventEnvelope_AppointmentCancelled{
			AppointmentCancelled: &eventsv1.AppointmentCancelled{
				AppointmentId: item.ID.String(),
				BranchId:      item.BranchID.String(),
				ClientId:      item.ClientID.String(),
				Reason:        reason,
			},
		},
	})
	// Отменённая запись освобождает слот — табло должно погаснуть обратно.
	s.announceSlot(ctx, item, false)
	return item, nil
}

// FreeSlots собирает сетку свободных слотов специалиста на окно.
func (s *Service) FreeSlots(
	ctx context.Context, branchID, employeeID, serviceID uuid.UUID, window Interval,
) ([]Slot, error) {
	if !window.Valid() {
		return nil, infra.InvalidArgument("конец окна должен быть позже начала")
	}

	service, err := s.directory.Service(ctx, serviceID, branchID)
	if err != nil {
		return nil, err
	}

	shifts, err := s.directory.EmployeeShifts(ctx, employeeID, window)
	if err != nil {
		return nil, err
	}

	taken, err := s.repo.TakenIntervals(ctx, employeeID, window)
	if err != nil {
		return nil, err
	}

	all := BuildSlots(shifts, taken, service.Duration, employeeID)
	free := make([]Slot, 0, len(all))
	for _, slot := range all {
		if !slot.Taken {
			free = append(free, slot)
		}
	}
	return free, nil
}
