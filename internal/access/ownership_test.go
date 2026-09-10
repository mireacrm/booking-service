package access

import (
	"testing"

	"github.com/mireacrm/go-common/infra"
)

func TestOwnership(t *testing.T) {
	const own = "3a7c9d21-0000-4000-8000-0000000000aa"
	const other = "3a7c9d21-0000-4000-8000-0000000000bb"

	cases := []struct {
		name    string
		caller  infra.Caller
		owner   string
		allowed bool
	}{
		{"специалист со своим объектом",
			infra.Caller{Subject: "s", Roles: []string{"specialist"}, EmployeeID: own}, own, true},
		{"специалист с чужим объектом",
			infra.Caller{Subject: "s", Roles: []string{"specialist"}, EmployeeID: own}, other, false},
		{"управляющий с чужим объектом",
			infra.Caller{Subject: "m", Roles: []string{"manager"}, EmployeeID: ""}, other, true},
		{"специалист без привязки к сотруднику",
			infra.Caller{Subject: "s", Roles: []string{"specialist"}}, other, false},
		{"вызов изнутри системы, без личности",
			infra.Caller{}, other, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := EnsureOwner(c.caller, c.owner, "визит")
			if c.allowed && err != nil {
				t.Errorf("ожидался доступ, получено: %v", err)
			}
			if !c.allowed && err == nil {
				t.Error("ожидался отказ, доступ разрешён")
			}
		})
	}
}
