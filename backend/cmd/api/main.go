package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"okfbundler/internal/adapters/auth"
	"okfbundler/internal/adapters/db"
	"okfbundler/internal/adapters/events"
	httpapi "okfbundler/internal/adapters/http"
	"okfbundler/internal/adapters/queue"
	"okfbundler/internal/adapters/storage"
	"okfbundler/internal/app"
	"okfbundler/internal/config"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	objectStore, err := storage.NewMinioStore(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioBucket, cfg.MinioUseSSL)
	if err != nil {
		log.Fatalf("minio: %v", err)
	}

	conn, err := queue.DialWithRetry(cfg.RabbitMQURL, 10, 2*time.Second)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("rabbitmq channel: %v", err)
	}
	defer ch.Close()

	if err := queue.Topology(ch); err != nil {
		log.Fatalf("rabbitmq topology: %v", err)
	}

	users := db.NewUserRepo(pool)
	documents := db.NewDocumentRepo(pool)
	jobs := db.NewJobRepo(pool)
	bundles := db.NewBundleRepo(pool)
	publisher := queue.NewPublisher(ch)
	tokens := auth.NewJWTIssuer(cfg.JWTSecret)

	hub := events.NewHub()
	go db.ListenJobStatus(ctx, cfg.PostgresDSN, hub)

	submit := &app.SubmitDocumentService{
		Documents: documents,
		Jobs:      jobs,
		Storage:   objectStore,
		Queue:     publisher,
	}

	deleteJob := &app.DeleteJobService{
		Jobs:      jobs,
		Documents: documents,
		Bundles:   bundles,
		Storage:   objectStore,
	}

	router := httpapi.NewRouter(httpapi.Dependencies{
		Submit:         submit,
		DeleteJob:      deleteJob,
		Jobs:           jobs,
		Bundles:        bundles,
		Storage:        objectStore,
		Tokens:         tokens,
		Users:          users,
		Hub:            hub,
		WebhookSecret:  cfg.WebhookSecret,
		FrontendOrigin: cfg.FrontendOrigin,
	})

	log.Printf("api escuchando en :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
