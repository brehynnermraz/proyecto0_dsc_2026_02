// Package config carga toda la configuración del servicio desde variables de
// entorno. Nada de valores incrustados en el código: es una exigencia del §7 del
// enunciado y lo que mantiene la configuración separada del binario.
//
// A diferencia del worker, este servicio no usa ninguna librería de parseo: son
// cuatro variables y la stdlib basta. Menos dependencias, imagen más pequeña.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// DefaultMaxObjectBytes es el tope por objeto cuando no se define MAX_OBJECT_BYTES.
// 50 MiB coincide con MAX_INPUT_BYTES del worker: no tiene sentido aceptar en el
// almacén un original que el worker rechazaría al procesarlo.
const DefaultMaxObjectBytes int64 = 52428800

type Config struct {
	// Addr es la dirección donde escucha el servidor HTTP, formato host:puerto.
	Addr string

	// Root es la carpeta raíz bajo la que se replica la jerarquía de claves. En
	// Compose debe ser un volumen nombrado: si fuera el disco del contenedor, los
	// objetos se perderían al reiniciarlo y se violaría la regla del §4 sobre no
	// depender del disco efímero.
	Root string

	// Token es el secreto compartido que exige el servicio en la cabecera
	// Authorization de toda ruta /v1. Sin él nadie escribe ni lee.
	Token string

	// MaxObjectBytes es el tamaño máximo aceptado por objeto. Un cliente que
	// intente subir más recibe 413.
	MaxObjectBytes int64
}

// Load lee el entorno, aplica los valores por defecto y valida el resultado.
func Load() (Config, error) {
	c := Config{
		Addr:           getenv("STORAGE_ADDR", ":9000"),
		Root:           getenv("STORAGE_ROOT", "/data/objects"),
		Token:          os.Getenv("STORAGE_TOKEN"),
		MaxObjectBytes: DefaultMaxObjectBytes,
	}

	if raw := os.Getenv("MAX_OBJECT_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return c, fmt.Errorf("MAX_OBJECT_BYTES no es un entero válido (%q): %w", raw, err)
		}
		c.MaxObjectBytes = n
	}

	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// Validate comprueba que la configuración es utilizable. Falla al arrancar en vez
// de dejar que el error aparezca en la primera petición: un servicio sin token o
// sin raíz nunca debería quedar escuchando.
func (c Config) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("STORAGE_ADDR es obligatorio")
	}
	if c.Root == "" {
		return fmt.Errorf("STORAGE_ROOT es obligatorio")
	}
	if c.Token == "" {
		return fmt.Errorf("STORAGE_TOKEN es obligatorio: sin él el servicio quedaría abierto a cualquiera")
	}
	if c.MaxObjectBytes <= 0 {
		return fmt.Errorf("MAX_OBJECT_BYTES debe ser > 0, es %d", c.MaxObjectBytes)
	}
	return nil
}

// getenv devuelve el valor de la variable o el respaldo si está vacía.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
