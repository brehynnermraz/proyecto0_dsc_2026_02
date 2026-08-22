// Command server arranca el servicio de almacenamiento de objetos.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/uniandes-isis4426/okf/objectstore/internal/config"
	"github.com/uniandes-isis4426/okf/objectstore/internal/httpapi"
	"github.com/uniandes-isis4426/okf/objectstore/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("el servicio terminó con error", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.NewFS(cfg.Root)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: httpapi.New(st, cfg, logger).Handler(),
		// Sin este timeout, un cliente que abre la conexión y no envía cabeceras la
		// mantendría ocupada para siempre (slowloris).
		ReadHeaderTimeout: 10 * time.Second,
	}

	// NotifyContext cancela ctx al recibir SIGINT/SIGTERM: es la señal de apagado
	// que envía `docker compose down`.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		logger.Info("almacén escuchando", "addr", cfg.Addr, "root", cfg.Root)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		logger.Info("apagando")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
