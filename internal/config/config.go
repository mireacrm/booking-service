package config

import "github.com/mireacrm/go-common/infra"

type Config struct {
	ServiceName  string
	PostgresDSN  string
	HTTPPort     int
	GRPCPort     int
	AMQPURL      string
	NATSURL      string
	CoreAddr     string
	CatalogAddr  string
	OTLPEndpoint string
	Debug        bool
}

func Load() (Config, error) {
	cfg := Config{
		ServiceName: "booking-service",
		PostgresDSN: infra.Env("BOOKING_POSTGRES_DSN", "postgres://booking_user:booking_pass@localhost:5432/booking_db"),
		AMQPURL:     infra.Env("BOOKING_AMQP_URL", "amqp://guest:guest@localhost:5672/"),
		NATSURL:     infra.Env("BOOKING_NATS_URL", "nats://localhost:4222"),
		CoreAddr:    infra.Env("BOOKING_CORE_ADDR", "localhost:9001"),
		CatalogAddr: infra.Env("BOOKING_CATALOG_ADDR", "localhost:9002"),
		// Пустой адрес выключает экспорт трасс.
		OTLPEndpoint: infra.Env("BOOKING_OTLP_ENDPOINT", ""),
		Debug:        infra.Env("BOOKING_DEBUG", "false") == "true",
	}

	var err error
	if cfg.HTTPPort, err = infra.EnvInt("BOOKING_HTTP_PORT", 8003); err != nil {
		return cfg, err
	}
	if cfg.GRPCPort, err = infra.EnvInt("BOOKING_GRPC_PORT", 9003); err != nil {
		return cfg, err
	}
	return cfg, nil
}
