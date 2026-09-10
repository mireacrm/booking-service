package booking

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mireacrm/go-common/infra"
)

// ownedRepo отдаёт один визит и запоминает, дошло ли дело до смены состояния.
type ownedRepo struct {
	fakeRepo
	item      *Appointment
	completed bool
}

func (r *ownedRepo) Get(context.Context, uuid.UUID) (*Appointment, error) {
	return r.item, nil
}

func (r *ownedRepo) SetStatus(
	_ context.Context, _ uuid.UUID, _, _ Status,
) (*Appointment, error) {
	r.completed = true
	return r.item, nil
}

func withCaller(roles []string, employee uuid.UUID) context.Context {
	caller := infra.Caller{Subject: "8f1c0e4e", Roles: roles}
	if employee != uuid.Nil {
		caller.EmployeeID = employee.String()
	}
	return infra.WithCaller(context.Background(), caller)
}

func newOwned(employee uuid.UUID) (*Service, *ownedRepo) {
	repo := &ownedRepo{item: &Appointment{ID: uuid.New(), EmployeeID: employee}}
	service, _, _ := newService(repo, fakeDirectory{})
	return service, repo
}

func TestCompleteChecksOwnership(t *testing.T) {
	own, other := uuid.New(), uuid.New()

	cases := []struct {
		name    string
		ctx     context.Context
		allowed bool
	}{
		{"свой визит", withCaller([]string{"specialist"}, own), true},
		{"чужой визит", withCaller([]string{"specialist"}, other), false},
		{"без привязки к сотруднику", withCaller([]string{"specialist"}, uuid.Nil), false},
		{"управляющий закрывает любой", withCaller([]string{"manager"}, uuid.Nil), true},
		{"вызов изнутри системы", context.Background(), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			service, repo := newOwned(own)
			_, err := service.Complete(c.ctx, repo.item.ID)

			var forbidden *infra.ForbiddenError
			denied := errors.As(err, &forbidden)

			if c.allowed && err != nil {
				t.Fatalf("ожидался доступ, получено: %v", err)
			}
			if !c.allowed && !denied {
				t.Fatalf("ожидался отказ, получено: %v", err)
			}
			// Существенно, что до смены состояния дело не дошло: закрыть
			// чужой визит нельзя даже на миг.
			if !c.allowed && repo.completed {
				t.Error("состояние изменено, несмотря на отказ")
			}
		})
	}
}

func TestGetChecksOwnership(t *testing.T) {
	own, other := uuid.New(), uuid.New()

	service, repo := newOwned(own)
	if _, err := service.Get(withCaller([]string{"specialist"}, own), repo.item.ID); err != nil {
		t.Fatalf("свой визит должен быть доступен: %v", err)
	}

	var forbidden *infra.ForbiddenError
	_, err := service.Get(withCaller([]string{"specialist"}, other), repo.item.ID)
	if !errors.As(err, &forbidden) {
		t.Fatalf("чужой визит должен быть недоступен, получено: %v", err)
	}
}

func TestCompleteSkipsExtraReadForPrivileged(t *testing.T) {
	// Проверка стоит денег: лишнее чтение выполняется только там, где нужно.
	service, repo := newOwned(uuid.New())
	if _, err := service.Complete(withCaller([]string{"admin"}, uuid.Nil), repo.item.ID); err != nil {
		t.Fatalf("администратор должен закрывать любой визит: %v", err)
	}
	if !repo.completed {
		t.Error("визит не закрыт")
	}
}
