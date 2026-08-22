// Package config carga toda la configuración desde variables de entorno.
// Nada de valores incrustados en el código: es una exigencia del §7 del enunciado.
//
// Solo están las variables que necesita la mensajería con RabbitMQ y el
// ciclo de vida del proceso. Los campos de Postgres, del lease llegan en
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// ---- AMQP ----
	AMQPURL        string        `env:"AMQP_URL,required"`
	Queue          string        `env:"AMQP_QUEUE"           envDefault:"jobs.convert"`
	Exchange       string        `env:"AMQP_EXCHANGE"        envDefault:"okf.jobs"`
	RoutingKey     string        `env:"AMQP_ROUTING_KEY"     envDefault:"convert"`
	ConsumerTag    string        `env:"AMQP_CONSUMER_TAG"    envDefault:""` // vacío = worker-{hostname}
	Prefetch       int           `env:"AMQP_PREFETCH"        envDefault:"1"`
	ReconnectDelay time.Duration `env:"AMQP_RECONNECT_DELAY" envDefault:"5s"`
	RetryTTL       time.Duration `env:"AMQP_RETRY_TTL"       envDefault:"30s"`
	DeliveryLimit  int           `env:"AMQP_DELIVERY_LIMIT"  envDefault:"20"`

	// ---- Simulación  ----
	// El worker todavía no convierte nada; simula el trabajo para poder ejercitar
	// la mensajería. SimulateWork es cuánto "tarda" cada trabajo; SimulateFailure
	// hace que todo trabajo devuelva un error transitorio, útil para ver el mensaje
	// caer en jobs.retry y volver a los 30 s.
	SimulateWork    time.Duration `env:"SIMULATE_WORK"    envDefault:"2s"`
	SimulateFailure bool          `env:"SIMULATE_FAILURE" envDefault:"false"`

	// ---- Almacenamiento de objetos ----
	// StorageBackend elige el adaptador que implementa el puerto Store:
	//   fs   → carpeta local (pruebas rápidas sin levantar el servicio).
	//   http → servicio object-storage por red (patrón real, sin compartir disco).
	// Con http hacen falta además StorageBaseURL y StorageToken (los valida Validate).
	StorageBackend string `env:"STORAGE_BACKEND"  envDefault:"fs"`
	StorageDir     string `env:"STORAGE_DIR"      envDefault:"./data/objects"` // backend fs
	StorageBaseURL string `env:"STORAGE_BASE_URL" envDefault:""`               // backend http, p. ej. http://localhost:9000
	StorageToken   string `env:"STORAGE_TOKEN"    envDefault:""`               // backend http, mismo secreto que exige el servicio

	// ---- Base de datos ----
	// Postgres es la fuente de verdad del estado del trabajo. DATABASE_URL es
	// obligatoria: sin base de datos el worker del paso 2 no arranca.
	DatabaseURL string `env:"DATABASE_URL,required"`
	PGMaxConns  int32  `env:"PG_MAX_CONNS" envDefault:"4"`

	// Lease/heartbeat: el worker renueva el lease cada HeartbeatInterval; si deja de
	// hacerlo durante LeaseTTL, otro worker puede re-tomar el trabajo. LeaseTTL debe
	// ser holgadamente mayor que HeartbeatInterval (varios latidos por lease).
	HeartbeatInterval time.Duration `env:"JOB_HEARTBEAT_INTERVAL" envDefault:"10s"`
	LeaseTTL          time.Duration `env:"JOB_LEASE_TTL"          envDefault:"30s"`
	JobTimeout        time.Duration `env:"JOB_TIMEOUT"            envDefault:"5m"`

	// MaxInputSize acota la lectura del original (entrada no confiable).
	MaxInputSize int64 `env:"MAX_INPUT_BYTES" envDefault:"52428800"` // 50 MiB

	// ---- Apagado y observabilidad ----
	ShutdownGrace time.Duration `env:"SHUTDOWN_GRACE" envDefault:"90s"`
	MetricsAddr   string        `env:"METRICS_ADDR"   envDefault:":9090"`
	LogLevel      string        `env:"LOG_LEVEL"      envDefault:"info"`
	LogFormat     string        `env:"LOG_FORMAT"     envDefault:"text"` // json | text
}

// Load lee el entorno y valida las relaciones entre valores.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return c, fmt.Errorf("leyendo configuración del entorno: %w", err)
	}

	// Sin etiqueta, las N réplicas aparecen idénticas en la UI de gestión. Con el
	// hostname —que en Docker es el ID del contenedor— cada una se distingue, que es
	// justo lo que hay que enseñar en el segmento de escalado del video.
	if c.ConsumerTag == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "desconocido"
		}
		c.ConsumerTag = "worker-" + host
	}

	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// Validate comprueba las relaciones entre valores. 
func (c Config) Validate() error {
	if c.Prefetch < 1 {
		return fmt.Errorf("AMQP_PREFETCH debe ser >= 1, es %d", c.Prefetch)
	}
	if c.RetryTTL <= 0 {
		return fmt.Errorf("AMQP_RETRY_TTL debe ser > 0, es %s", c.RetryTTL)
	}

	switch c.StorageBackend {
	case "fs":
		// StorageDir tiene default; nada más que exigir.
	case "http":
		// Fallar al arrancar, no en el primer Get/Put contra el servicio.
		if c.StorageBaseURL == "" {
			return fmt.Errorf("STORAGE_BASE_URL es obligatorio con STORAGE_BACKEND=http")
		}
		if c.StorageToken == "" {
			return fmt.Errorf("STORAGE_TOKEN es obligatorio con STORAGE_BACKEND=http")
		}
	default:
		return fmt.Errorf("STORAGE_BACKEND debe ser fs|http, es %q", c.StorageBackend)
	}

	// El lease tiene que sobrevivir a varios latidos; si no, el trabajo se declararía
	// "muerto" mientras el worker sigue vivo y otro lo re-tomaría en paralelo.
	if c.LeaseTTL <= c.HeartbeatInterval {
		return fmt.Errorf("JOB_LEASE_TTL (%s) debe ser mayor que JOB_HEARTBEAT_INTERVAL (%s)",
			c.LeaseTTL, c.HeartbeatInterval)
	}
	if c.MaxInputSize <= 0 {
		return fmt.Errorf("MAX_INPUT_BYTES debe ser > 0, es %d", c.MaxInputSize)
	}
	return nil
}
