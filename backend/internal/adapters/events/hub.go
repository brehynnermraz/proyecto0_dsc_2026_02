package events

import "sync"

type JobStatusEvent struct {
	JobID  string
	Status string
}

// Hub reparte eventos de estado de job a quien esté suscrito a ese job_id
// concreto, dentro de un mismo proceso de la API. La fuente de los eventos
// (Postgres LISTEN/NOTIFY, ver internal/adapters/db/listener.go) es
// intercambiable sin tocar esto.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan JobStatusEvent]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan JobStatusEvent]struct{})}
}

func (h *Hub) Subscribe(jobID string) chan JobStatusEvent {
	ch := make(chan JobStatusEvent, 4)

	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[chan JobStatusEvent]struct{})
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()

	return ch
}

func (h *Hub) Unsubscribe(jobID string, ch chan JobStatusEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.subs[jobID][ch]; !ok {
		return
	}
	delete(h.subs[jobID], ch)
	if len(h.subs[jobID]) == 0 {
		delete(h.subs, jobID)
	}
	close(ch)
}

func (h *Hub) Publish(evt JobStatusEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subs[evt.JobID] {
		select {
		case ch <- evt:
		default: // un suscriptor lento no debe bloquear al publicador
		}
	}
}
