package booking

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// SlotStep — шаг сетки расписания. Записи выравниваются по нему, иначе
// расписание рассыпается на неиспользуемые огрызки в пять минут.
const SlotStep = 30 * time.Minute

// BuildSlots нарезает смены специалиста на сетку и помечает занятые.
// Смены и записи приходят из разных источников: смены — из core-service,
// записи — из своей базы.
func BuildSlots(shifts []Interval, taken []Interval, duration time.Duration, employeeID uuid.UUID) []Slot {
	slots := make([]Slot, 0)

	for _, shift := range shifts {
		for start := alignUp(shift.Start); !start.Add(duration).After(shift.End); start = start.Add(SlotStep) {
			candidate := Interval{Start: start, End: start.Add(duration)}
			slots = append(slots, Slot{
				EmployeeID: employeeID,
				StartsAt:   candidate.Start,
				EndsAt:     candidate.End,
				Taken:      overlapsAny(candidate, taken),
			})
		}
	}

	sort.Slice(slots, func(i, j int) bool { return slots[i].StartsAt.Before(slots[j].StartsAt) })
	return slots
}

// FitsShift проверяет, что запись целиком укладывается в одну смену.
// Запись, растянутая на две смены с перерывом между ними, недопустима.
func FitsShift(shifts []Interval, wanted Interval) bool {
	for _, shift := range shifts {
		if shift.Contains(wanted) {
			return true
		}
	}
	return false
}

func overlapsAny(candidate Interval, intervals []Interval) bool {
	for _, item := range intervals {
		if candidate.Overlaps(item) {
			return true
		}
	}
	return false
}

func alignUp(value time.Time) time.Time {
	rounded := value.Truncate(SlotStep)
	if rounded.Before(value) {
		return rounded.Add(SlotStep)
	}
	return rounded
}
