package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"okfbundler/internal/adapters/converter"
	"okfbundler/internal/adapters/db"
	"okfbundler/internal/adapters/queue"
	"okfbundler/internal/adapters/storage"
	workeradapter "okfbundler/internal/adapters/worker"
	"okfbundler/internal/app"
	"okfbundler/internal/config"
	"okfbundler/internal/ports"
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

	process := &app.ProcessJobService{
		Jobs:      db.NewJobRepo(pool),
		Documents: db.NewDocumentRepo(pool),
		Bundles:   db.NewBundleRepo(pool),
		Storage:   objectStore,
		Converters: []ports.DocumentConverter{
			converter.MarkdownConverter{},
			converter.PlainTextConverter{},
			converter.HTMLConverter{},
		},
	}

	// "worker x N" del diagrama: cada worker abre su propio canal AMQP y
	// consume con prefetch=1, así uno lento no acapara mensajes que otro
	// podría procesar ya.
	concurrency := envInt("WORKER_CONCURRENCY", 3)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		ch, err := conn.Channel()
		if err != nil {
			log.Fatalf("rabbitmq channel %d: %v", i, err)
		}
		if err := queue.Topology(ch); err != nil {
			log.Fatalf("rabbitmq topology: %v", err)
		}
		if err := ch.Qos(1, 0, false); err != nil {
			log.Fatalf("qos: %v", err)
		}

		consumer := &workeradapter.Consumer{Channel: ch, Process: process}
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			log.Printf("worker %d escuchando", id)
			if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("worker %d detenido con error: %v", id, err)
			}
		}(i)
	}

	<-ctx.Done()
	log.Println("apagando workers...")
	wg.Wait()
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
