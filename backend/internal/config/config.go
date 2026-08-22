package config

import "os"

type Config struct {
	Port        string
	PostgresDSN string
	RabbitMQURL string

	// Object store: el servicio object-storage (../object-storage), el MISMO
	// almacén que usa el worker. Sustituye a MinIO.
	StorageBaseURL string // p. ej. http://localhost:9000
	StorageToken   string // STORAGE_TOKEN compartido con el servicio y el worker

	JWTSecret      string
	FrontendOrigin string
}

func Load() Config {
	return Config{
		Port:           env("PORT", "8080"),
		PostgresDSN:    env("POSTGRES_DSN", "postgres://okf:okf@postgres:5432/okf?sslmode=disable"),
		RabbitMQURL:    env("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		StorageBaseURL: env("STORAGE_BASE_URL", "http://object-storage:9000"),
		StorageToken:   env("STORAGE_TOKEN", ""),
		JWTSecret:      env("JWT_SECRET", ""),
		FrontendOrigin: env("FRONTEND_ORIGIN", "http://localhost:3000"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
