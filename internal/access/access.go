// Package access — правило доступа к объектам сервиса.
//
// Роль проверяет шлюз, принадлежность — сервис-владелец: только он знает, чей
// это объект. В общий модуль правило не вынесено намеренно: у каждого сервиса
// свои объекты, и общая на всех проверка означала бы, что правило меняется
// у всех сразу.
package access

import "github.com/mireacrm/go-common/infra"

// Роли, которым видно чужое. Специалист работает только со своим.
var privilegedRoles = []string{"admin", "manager"}

func Privileged(caller infra.Caller) bool { return caller.HasAny(privilegedRoles...) }

// EnsureOwner пропускает вызов, если объект принадлежит вызывающему.
//
// Вызов без личности — обращение изнутри системы, а не от человека:
// потребитель события или служебная задача. Снаружи такой вызов не сделать,
// заголовки личности шлюз затирает.
func EnsureOwner(caller infra.Caller, ownerID, what string) error {
	if !caller.Known() || Privileged(caller) {
		return nil
	}
	// Пустая привязка означает, что учётной записи не соответствует ни один
	// сотрудник. Отказ по умолчанию: иначе такая учётка видела бы всё.
	if caller.EmployeeID == "" || caller.EmployeeID != ownerID {
		return infra.Forbidden(what)
	}
	return nil
}
