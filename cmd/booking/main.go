package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mireacrm/go-common/infra"
	"github.com/mireacrm/booking-service/internal/api"
	"github.com/mireacrm/booking-service/internal/booking"
	"github.com/mireacrm/booking-service/internal/clients"
	"github.com/mireacrm/booking-service/internal/config"
	"github.com/mireacrm/booking-service/internal/rpc"
	"github.com/mireacrm/booking-service/migrations"
)

func main() {
	// В distroless-образе нет ни shell, ни curl, поэтому healthcheck
	// контейнера выполняет сам бинарник.
	healthcheck := flag.Bool("healthcheck", false, "проверить готовность и выйти")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}

	if err := run(); err != nil {
		slog.Error("сервис остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg.Debug)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := infra.SetupTracing(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	// Экспортёр копит спаны пачками: без остановки последняя пачка теряется.
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	if err := infra.Migrate(ctx, cfg.PostgresDSN, migrations.Files); err != nil {
		return err
	}
	slog.Info("миграции накачены")

	pool, err := infra.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	directory, err := clients.DialDirectory(cfg.CoreAddr, cfg.CatalogAddr)
	if err != nil {
		return err
	}
	defer directory.Close()

	events, err := infra.NewEventPublisher(cfg.AMQPURL, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer events.Close()

	realtime, err := infra.NewRealtimePublisher(cfg.NATSURL, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer realtime.Close()

	service := booking.New(booking.NewRepository(pool), directory, events, realtime)

	router := api.NewRouter(service, cfg.ServiceName,
		infra.Probe{Name: "database", Check: pool.Ping},
		infra.Probe{Name: "broker", Check: events.Ping},
		infra.Probe{Name: "realtime", Check: realtime.Ping},
	)

	grpcServer := infra.NewGRPCServer(rpc.Register(service))
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.GRPCPort))
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.HTTPPort),
		Handler: infra.HTTPHandler(router, cfg.ServiceName),
	}

	errc := make(chan error, 2)
	go func() {
		slog.Info("HTTP слушает", "port", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() {
		slog.Info("gRPC слушает", "port", cfg.GRPCPort)
		if err := grpcServer.Serve(listener); err != nil {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("получен сигнал, останавливаемся")
	}

	grpcServer.GracefulStop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), infra.ShutdownGrace)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func probe() int {
	cfg, err := config.Load()
	if err != nil {
		return 1
	}

	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(cfg.HTTPPort) + "/readyz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func setupLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
