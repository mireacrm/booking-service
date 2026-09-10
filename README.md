# Сервис записи

Записи клиентов к специалистам: подбор свободного времени, создание
и отмена записи, завершение визита. Ядро синхронного сценария — ходит
в каталог и справочник клиентов по gRPC, о завершении визита сообщает
событием.

Пересечения записей запрещены на уровне базы ограничением `EXCLUDE`,
поэтому базе нужно расширение `btree_gist`.

## Зависимости

| Модуль | Роль |
|---|---|
| [`mireacrm/contracts-go`](https://github.com/mireacrm/contracts-go) | сообщения и стабы gRPC |
| [`mireacrm/go-common`](https://github.com/mireacrm/go-common) | транспорт, трассировка, метрики, каркас процесса |

## Локально

```
go test ./...                    # модульные
go test -tags integration ./...  # нужен Postgres
docker build -t booking-service .
```

Систему целиком поднимает [`mireacrm/deploy`](https://github.com/mireacrm/deploy).
