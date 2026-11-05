package notify

import (
	"sync"

	"devices-api/internal/device"
)

// Broker is an in-process fan-out for state change events. Subscribers are
// intentionally non-blocking: a slow client must not stall an API request.
// A production implementation would use a durable broker or a transactional
// outbox; the challenge explicitly does not require replay after restarts.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan device.StateChangedEvent]struct{}
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[chan device.StateChangedEvent]struct{})}
}

// Subscribe returns a buffered channel and a cancel function. The channel is
// closed on cancel so readers can stop safely.
func (b *Broker) Subscribe() (<-chan device.StateChangedEvent, func()) {
	ch := make(chan device.StateChangedEvent, 16)

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[ch]; ok {
			delete(b.subscribers, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// Publish implements device.Notifier.
func (b *Broker) Publish(event device.StateChangedEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Drop for slow consumers instead of blocking the update path.
		}
	}
}
