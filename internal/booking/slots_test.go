package booking

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var day = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

func at(hour, minute int) time.Time {
	return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

func iv(fromHour, fromMin, toHour, toMin int) Interval {
	return Interval{Start: at(fromHour, fromMin), End: at(toHour, toMin)}
}

func TestIntervalOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b Interval
		want bool
	}{
		{"пересекаются", iv(9, 0, 18, 0), iv(14, 0, 20, 0), true},
		{"вложен", iv(9, 0, 18, 0), iv(12, 0, 14, 0), true},
		{"встык не пересечение", iv(9, 0, 14, 0), iv(14, 0, 20, 0), false},
		{"не пересекаются", iv(9, 0, 12, 0), iv(14, 0, 16, 0), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Fatalf("Overlaps = %v, want %v", got, tc.want)
			}
			if got := tc.b.Overlaps(tc.a); got != tc.want {
				t.Fatalf("несимметрично: %v", got)
			}
		})
	}
}

func TestBuildSlotsAlignsToGrid(t *testing.T) {
	// Смена начинается в 9:10, сетка получасовая — первый слот в 9:30,
	// иначе расписание рассыпается на огрызки.
	slots := BuildSlots([]Interval{iv(9, 10, 12, 0)}, nil, time.Hour, uuid.New())

	if len(slots) == 0 {
		t.Fatal("слотов нет")
	}
	if got := slots[0].StartsAt; !got.Equal(at(9, 30)) {
		t.Fatalf("первый слот %v, ожидался 9:30", got.Format("15:04"))
	}
	if last := slots[len(slots)-1]; last.EndsAt.After(at(12, 0)) {
		t.Fatalf("последний слот вылезает за смену: %v", last.EndsAt.Format("15:04"))
	}
}

func TestBuildSlotsMarksTaken(t *testing.T) {
	slots := BuildSlots(
		[]Interval{iv(9, 0, 12, 0)},
		[]Interval{iv(10, 0, 11, 0)},
		time.Hour,
		uuid.New(),
	)

	for _, slot := range slots {
		candidate := Interval{Start: slot.StartsAt, End: slot.EndsAt}
		want := candidate.Overlaps(iv(10, 0, 11, 0))
		if slot.Taken != want {
			t.Fatalf("слот %v: Taken=%v, want %v", slot.StartsAt.Format("15:04"), slot.Taken, want)
		}
	}
}

func TestBuildSlotsSkipsShiftShorterThanService(t *testing.T) {
	slots := BuildSlots([]Interval{iv(9, 0, 9, 30)}, nil, time.Hour, uuid.New())

	if len(slots) != 0 {
		t.Fatalf("услуга длиннее смены, слотов быть не должно: %d", len(slots))
	}
}

func TestFitsShift(t *testing.T) {
	shifts := []Interval{iv(9, 0, 13, 0), iv(14, 0, 18, 0)}

	cases := []struct {
		name   string
		wanted Interval
		want   bool
	}{
		{"внутри первой смены", iv(9, 0, 10, 0), true},
		{"ровно по границе", iv(14, 0, 18, 0), true},
		{"в перерыве", iv(13, 0, 14, 0), false},
		{"через перерыв", iv(12, 0, 15, 0), false},
		{"вылезает за конец", iv(17, 0, 19, 0), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FitsShift(shifts, tc.wanted); got != tc.want {
				t.Fatalf("FitsShift = %v, want %v", got, tc.want)
			}
		})
	}
}
