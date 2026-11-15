// Package events verteilt Ereignisse an Web-Clients (WebSocket).
package events

import "sync"

// Message ist ein Ereignis. Typen: "job.updated", "disks.changed", "pools.changed".
type Message struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

const bufferSize = 64

// Hub verteilt Nachrichten an alle Abonnenten. Langsame Abonnenten verlieren Nachrichten,
// statt den Sender zu blockieren.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Message]struct{}
}

// NewHub erzeugt einen Hub.
func NewHub() *Hub { return &Hub{subs: map[chan Message]struct{}{}} }

// Publish sendet m an alle Abonnenten, ohne zu blockieren.
func (h *Hub) Publish(m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- m:
		default:
		}
	}
}

// Subscribe liefert einen Kanal und eine Funktion zum Abmelden (schließt den Kanal).
func (h *Hub) Subscribe() (<-chan Message, func()) {
	ch := make(chan Message, bufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}
