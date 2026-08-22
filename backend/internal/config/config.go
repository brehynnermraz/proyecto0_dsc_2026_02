package config

import "os"

type Config struct {
	Port           string
	PostgresDSN    string
	RabbitMQURL    string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioUseSSL    bool
	JWTSecret      string
	WebhookSecret  string
	FrontendOrigin string
}

func Load() Config {
	return Config{
		Port:           env("PORT", "8080"),
		PostgresDSN:    env("POSTGRES_DSN", "postgres://okf:okf@postgres:5432/okf?sslmode=disable"),
		RabbitMQURL:    env("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		MinioEndpoint:  env("MINIO_ENDPOINT", "minio:9000"),
		MinioAccessKey: env("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey: env("MINIO_SECRET_KEY", "minioadmin"),
		MinioBucket:    env("MINIO_BUCKET", "okf"),
		MinioUseSSL:    env("MINIO_USE_SSL", "false") == "true",
		JWTSecret:      env("JWT_SECRET", ""),
		WebhookSecret:  env("WORKER_WEBHOOK_SECRET", ""),
		FrontendOrigin: env("FRONTEND_ORIGIN", "http://localhost:3000"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
