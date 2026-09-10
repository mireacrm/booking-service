package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mireacrm/booking-service/internal/booking"
	"github.com/mireacrm/go-common/infra"
)

type Handler struct {
	service *booking.Service
}

func NewRouter(
	service *booking.Service, serviceName string, probes ...infra.Probe,
) http.Handler {
	handler := &Handler{service: service}

	router := chi.NewRouter()
	router.Use(infra.TraceMiddleware, infra.IdentityMiddleware,
		infra.MetricsMiddleware(serviceName))

	router.Get("/branches/{branchID}/slots", handler.freeSlots)
	router.Post("/appointments", handler.book)
	router.Get("/appointments/{appointmentID}", handler.get)
	router.Post("/appointments/{appointmentID}/complete", handler.complete)
	router.Post("/appointments/{appointmentID}/cancel", handler.cancel)

	router.Get("/metrics", infra.MetricsHandler().ServeHTTP)
	router.Get("/healthz", infra.LivenessHandler())
	router.Get("/readyz", infra.ReadinessHandler(probes...))
	return router
}

func (h *Handler) freeSlots(w http.ResponseWriter, r *http.Request) {
	branchID, err := pathUUID(r, "branchID")
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}

	employeeID, err := queryUUID(r, "employee_id")
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}
	serviceID, err := queryUUID(r, "service_id")
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}

	window, err := queryWindow(r)
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}

	slots, err := h.service.FreeSlots(r.Context(), branchID, employeeID, serviceID, window)
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}
	infra.WriteJSON(w, http.StatusOK, slotsOut(slots))
}

func (h *Handler) book(w http.ResponseWriter, r *http.Request) {
	var request BookRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		infra.WriteError(w, r, infra.InvalidArgument("тело запроса: %v", err))
		return
	}

	appointment, err := h.service.Book(r.Context(), booking.BookRequest{
		BranchID:   request.BranchID,
		ClientID:   request.ClientID,
		EmployeeID: request.EmployeeID,
		ServiceID:  request.ServiceID,
		StartsAt:   request.StartsAt,
	})
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}
	infra.WriteJSON(w, http.StatusCreated, appointmentOut(appointment))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	h.byID(w, r, h.service.Get, http.StatusOK)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	h.byID(w, r, h.service.Complete, http.StatusOK)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	// Причина необязательна: клиент может просто передумать.
	var body CancelRequest
	_ = json.NewDecoder(r.Body).Decode(&body)

	h.byID(w, r, func(ctx context.Context, id uuid.UUID) (*booking.Appointment, error) {
		return h.service.Cancel(ctx, id, body.Reason)
	}, http.StatusOK)
}

type byIDFunc func(ctx context.Context, id uuid.UUID) (*booking.Appointment, error)

func (h *Handler) byID(w http.ResponseWriter, r *http.Request, action byIDFunc, status int) {
	id, err := pathUUID(r, "appointmentID")
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}

	appointment, err := action(r.Context(), id)
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}
	infra.WriteJSON(w, status, appointmentOut(appointment))
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, infra.InvalidArgument("%s: невалидный UUID", name)
	}
	return id, nil
}

func queryUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.URL.Query().Get(name))
	if err != nil {
		return uuid.Nil, infra.InvalidArgument("%s: невалидный UUID", name)
	}
	return id, nil
}

func queryWindow(r *http.Request) (booking.Interval, error) {
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		return booking.Interval{}, infra.InvalidArgument("from: ожидается RFC3339")
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		return booking.Interval{}, infra.InvalidArgument("to: ожидается RFC3339")
	}
	return booking.Interval{Start: from, End: to}, nil
}
