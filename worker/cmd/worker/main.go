// Command worker consume trabajos de RabbitMQ. Flujo es real de punta
// a punta con base de datos: toma el trabajo en Postgres (Claim), lee el original del
// object store (servicio http), lo convierte y publica el bundle en una
// transacción.
//
// Es la composition root: el único lugar donde se decide qué implementación concreta
// se enchufa en cada puerto. Todo lo demás del programa habla con interfaces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/uniandes-isis4426/okf/worker/internal/adapter/amqp"
	"github.com/uniandes-isis4426/okf/worker/internal/adapter/apiclient"
	"github.com/uniandes-isis4426/okf/worker/internal/adapter/db"
	"github.com/uniandes-isis4426/okf/worker/internal/adapter/objectstore"
	"github.com/uniandes-isis4426/okf/worker/internal/config"
	"github.com/uniandes-isis4426/okf/worker/internal/job"
	"github.com/uniandes-isis4426/okf/worker/internal/logging"
)

func main() {
	// La imagen será distroless: no hay curl ni sh para el healthcheck de
	// Compose, así que el propio binario hace de cliente contra su servidor de salud.
	healthcheck := flag.Bool("healthcheck", false, "consulta /healthz y termina")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.Setup(cfg.LogLevel, cfg.LogFormat)
	log.Info("iniciando worker",
		"go", runtime.Version(),
		// GOMAXPROCS sale del límite de CPU del cgroup: desde Go 1.25 el runtime lo
		// lee y se ajusta solo. Fijar la variable de entorno DESACTIVA ese
		// comportamiento, así que no se toca; basta con `cpus` en Compose .
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"queue", cfg.Queue,
		"storage_backend", cfg.StorageBackend,
		"lease_ttl", cfg.LeaseTTL,
		"heartbeat", cfg.HeartbeatInterval,
	)

	// SIGTERM es lo que manda Docker al escalar hacia abajo o al parar.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ---- Adaptador secundario: base de datos (fuente de verdad del estado) ----
	repo, err := db.New(ctx, cfg.DatabaseURL, cfg.PGMaxConns)
	if err != nil {
		return err
	}
	defer repo.Close()
	log.Info("conectado a Postgres", "max_conns", cfg.PGMaxConns)

	// ---- Adaptador secundario: object store (fs local o servicio http) ----
	// El backend concreto lo decide STORAGE_BACKEND; el núcleo solo ve el puerto Store.
	store, err := objectstore.New(cfg)
	if err != nil {
		return err
	}

	// ---- Adaptador secundario: notificación best-effort a la API (hoy, log) ----
	notifier := apiclient.NewLogNotifier(log)

	// ---- Núcleo: procesador real (claim → convertir → publicar transaccional) ----
	processor := job.NewProcessor(repo, store, notifier, cfg, log)

	// ---- Adaptador primario ----
	consumer := amqp.NewConsumer(cfg, processor, log)

	health := startHealthServer(cfg, consumer, log)

	errCh := make(chan error, 1)
	go func() { errCh <- consumer.Run(ctx) }()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		log.Info("señal de apagado recibida, terminando el trabajo en curso",
			"grace", cfg.ShutdownGrace)
	}

	// Apagado ordenado: no se toma trabajo nuevo y se espera al que hay en curso.
	consumer.Wait(cfg.ShutdownGrace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = health.Shutdown(shutdownCtx)

	log.Info("worker detenido")
	return nil
}

// startHealthServer expone /healthz y /readyz. NO se publica al host: Compose y
// Prometheus llegan por la red interna. Publicar un puerto rompería `--scale`..
func startHealthServer(cfg config.Config, c *amqp.Consumer, log logger) *http.Server {
	mux := http.NewServeMux()

	// Vivo: el proceso responde.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Listo: además hay conexión con el broker. Un worker sin broker no sirve para
	// nada aunque el proceso siga en pie.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !c.Alive() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "sin conexión con el broker")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: cfg.MetricsAddr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor de salud detenido", "err", err)
		}
	}()
	return srv
}

// logger es lo mínimo que startHealthServer necesita; evita acoplar la firma a slog.
type logger interface {
	Error(msg string, args ...any)
}

// runHealthcheck es el modo cliente del flag -healthcheck.
func runHealthcheck() int {
	addr := os.Getenv("METRICS_ADDR")
	if addr == "" {
		addr = ":9090"
	}
	cli := &http.Client{Timeout: 2 * time.Second}
	resp, err := cli.Get("http://localhost" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthz devolvió %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
