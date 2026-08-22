// Package job orquesta el flujo del worker. Sus errores centinela son lo que el
// receptor de RabbitMQ (adapter/amqp) inspecciona para decidir ack o nack.
//
// Regla: envolver siempre con %w (o con Permanent) para que errors.Is los encuentre.
package job

import "errors"

var (
	// ErrTransient — fallo recuperable (red caída, Postgres o el store temporalmente
	// inaccesibles, timeout). El receptor hace nack: el mensaje va a jobs.retry y
	// vuelve tras el TTL. Es también el caso por defecto: cualquier error que NO sea
	// uno de los centinelas de abajo se trata como transitorio.
	ErrTransient = errors.New("fallo transitorio")

	// ErrPermanent — reintentar no arreglaría nada (MIME no soportado, archivo
	// corrupto, original inexistente, bundle inválido). El receptor hace ack; el
	// usuario podría pedir el reintento explícitamente desde la API.
	ErrPermanent = errors.New("fallo permanente")

	// ErrNotClaimable — el trabajo no se pudo tomar. Cubre tres casos a la vez:
	// reentrega duplicada, trabajo ya terminado y trabajo cancelado. NO es un fallo:
	// se hace ack y se descarta.
	ErrNotClaimable = errors.New("el trabajo no se puede tomar")

	// ErrExhausted — se agotó max_attempts. El worker lo marca 'dead' y hace ack.
	ErrExhausted = errors.New("intentos agotados")

	// ErrCancelled — el latido (heartbeat) devolvió cero filas a mitad de la
	// conversión: el trabajo se canceló o terminó por otra vía. Se hace ack; el estado
	// ya lo fijó quien canceló.
	ErrCancelled = errors.New("trabajo cancelado")

	// ErrAlreadyPublished — al publicar, el ON CONFLICT (job_id) DO NOTHING no insertó:
	// otro worker ya publicó este bundle. No es un fallo: se hace ack y se sigue. Lo
	// devuelve el repositorio; el procesador lo trata como éxito.
	ErrAlreadyPublished = errors.New("el bundle ya estaba publicado")
)

// Permanent envuelve un error como permanente, conservando el mensaje original (útil
// para el campo `error` de la tabla jobs) y haciendo que errors.Is(x, ErrPermanent)
// sea verdadero.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string        { return e.err.Error() }
func (e *permanentError) Unwrap() error        { return e.err }
func (e *permanentError) Is(target error) bool { return target == ErrPermanent }
