package review

import "sync"

// Event is one thing the UI should hear about.
type Event struct {
	// Type is review (a review changed: fetch it again), queue (the list
	// changed), activity (an agent progress line) or ask (answer text).
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
}

// Hub fans events out to every open window.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan Event]struct{}{}} }

// Subscribe returns a channel of events and a function to stop.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Send delivers e to every subscriber, dropping it for one that is not
// keeping up: the next review event makes that window refetch anyway.
func (h *Hub) Send(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
