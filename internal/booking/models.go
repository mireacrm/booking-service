package booking

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusNoShow    Status = "no_show"
)

func (s Status) Valid() bool {
	switch s {
	case StatusScheduled, StatusCompleted, StatusCancelled, StatusNoShow:
		return true
	}
	return false
}

// Appointment — запись клиента к специалисту. Официальный термин «запись»,
// «визит» в коде не употребляется (см. docs/glossary.md).
type Appointment struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	ClientID     uuid.UUID
	EmployeeID   uuid.UUID
	ServiceID    uuid.UUID
	StartsAt     time.Time
	EndsAt       time.Time
	Status       Status
	PriceKopecks int64
	CreatedAt    time.Time
}

// Slot — отрезок расписания специалиста с признаком занятости.
type Slot struct {
	EmployeeID uuid.UUID
	StartsAt   time.Time
	EndsAt     time.Time
	Taken      bool
}

// Interval — полуоткрытый [Start, End). Смены встык не считаются пересечением.
type Interval struct {
	Start time.Time
	End   time.Time
}

func (i Interval) Overlaps(other Interval) bool {
	return i.Start.Before(other.End) && other.Start.Before(i.End)
}

func (i Interval) Contains(other Interval) bool {
	return !i.Start.After(other.Start) && !i.End.Before(other.End)
}

func (i Interval) Valid() bool { return i.End.After(i.Start) }
