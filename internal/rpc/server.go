package rpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	bookingv1 "github.com/mireacrm/contracts-go/mirea/booking/v1"
	commonv1 "github.com/mireacrm/contracts-go/mirea/common/v1"

	"github.com/mireacrm/booking-service/internal/booking"
	"github.com/mireacrm/go-common/infra"
)

type Server struct {
	bookingv1.UnimplementedBookingServiceServer
	service *booking.Service
}

func Register(service *booking.Service) infra.Registration {
	return func(server *grpc.Server) {
		bookingv1.RegisterBookingServiceServer(server, &Server{service: service})
	}
}

func (s *Server) GetAppointment(
	ctx context.Context, request *bookingv1.GetAppointmentRequest,
) (*bookingv1.GetAppointmentResponse, error) {
	id, err := parseUUID(request.GetAppointmentId(), "appointment_id")
	if err != nil {
		return nil, err
	}

	item, err := s.service.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &bookingv1.GetAppointmentResponse{Appointment: appointment(item)}, nil
}

func (s *Server) ListClientAppointments(
	ctx context.Context, request *bookingv1.ListClientAppointmentsRequest,
) (*bookingv1.ListClientAppointmentsResponse, error) {
	clientID, err := parseUUID(request.GetClientId(), "client_id")
	if err != nil {
		return nil, err
	}

	items, next, err := s.service.ListByClient(
		ctx, clientID, int(request.GetPage().GetLimit()), request.GetPage().GetCursor(),
	)
	if err != nil {
		return nil, err
	}

	return &bookingv1.ListClientAppointmentsResponse{
		Appointments: appointments(items),
		Page:         &commonv1.PageResponse{NextCursor: next},
	}, nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, infra.InvalidArgument("%s: невалидный UUID", field)
	}
	return id, nil
}
