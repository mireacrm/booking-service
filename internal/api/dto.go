package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/mireacrm/booking-service/internal/booking"
)

// Представления домена в HTTP-ответах. Аналог internal/rpc/mapping.go для gRPC.

type BookRequest struct {
	BranchID   uuid.UUID `json:"branch_id"`
	ClientID   uuid.UUID `json:"client_id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	ServiceID  uuid.UUID `json:"service_id"`
	StartsAt   time.Time `json:"starts_at"`
}

type CancelRequest struct {
	Reason string `json:"reason"`
}

type AppointmentOut struct {
	ID           uuid.UUID `json:"id"`
	BranchID     uuid.UUID `json:"branch_id"`
	ClientID     uuid.UUID `json:"client_id"`
	EmployeeID   uuid.UUID `json:"employee_id"`
	ServiceID    uuid.UUID `json:"service_id"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Status       string    `json:"status"`
	PriceKopecks int64     `json:"price_kopecks"`
}

type SlotOut struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}

func appointmentOut(item *booking.Appointment) AppointmentOut {
	return AppointmentOut{
		ID:           item.ID,
		BranchID:     item.BranchID,
		ClientID:     item.ClientID,
		EmployeeID:   item.EmployeeID,
		ServiceID:    item.ServiceID,
		StartsAt:     item.StartsAt,
		EndsAt:       item.EndsAt,
		Status:       string(item.Status),
		PriceKopecks: item.PriceKopecks,
	}
}

func slotsOut(slots []booking.Slot) []SlotOut {
	out := make([]SlotOut, 0, len(slots))
	for _, slot := range slots {
		out = append(out, SlotOut{
			EmployeeID: slot.EmployeeID,
			StartsAt:   slot.StartsAt,
			EndsAt:     slot.EndsAt,
		})
	}
	return out
}
