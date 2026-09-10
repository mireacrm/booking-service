package rpc

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	bookingv1 "github.com/mireacrm/contracts-go/mirea/booking/v1"
	commonv1 "github.com/mireacrm/contracts-go/mirea/common/v1"

	"github.com/mireacrm/booking-service/internal/booking"
)

// Перевод доменных моделей в сообщения protobuf. Аналог internal/api/dto.go
// для HTTP: домен про транспорт ничего не знает.

var statusToProto = map[booking.Status]bookingv1.AppointmentStatus{
	booking.StatusScheduled: bookingv1.AppointmentStatus_APPOINTMENT_STATUS_SCHEDULED,
	booking.StatusCompleted: bookingv1.AppointmentStatus_APPOINTMENT_STATUS_COMPLETED,
	booking.StatusCancelled: bookingv1.AppointmentStatus_APPOINTMENT_STATUS_CANCELLED,
	booking.StatusNoShow:    bookingv1.AppointmentStatus_APPOINTMENT_STATUS_NO_SHOW,
}

func appointment(item *booking.Appointment) *bookingv1.Appointment {
	return &bookingv1.Appointment{
		Id:         item.ID.String(),
		BranchId:   item.BranchID.String(),
		ClientId:   item.ClientID.String(),
		EmployeeId: item.EmployeeID.String(),
		ServiceId:  item.ServiceID.String(),
		Period: &commonv1.TimeRange{
			StartAt: timestamppb.New(item.StartsAt),
			EndAt:   timestamppb.New(item.EndsAt),
		},
		Status: statusToProto[item.Status],
		Price: &commonv1.Money{
			AmountKopecks: item.PriceKopecks,
			CurrencyCode:  "RUB",
		},
	}
}

func appointments(items []*booking.Appointment) []*bookingv1.Appointment {
	out := make([]*bookingv1.Appointment, 0, len(items))
	for _, item := range items {
		out = append(out, appointment(item))
	}
	return out
}
