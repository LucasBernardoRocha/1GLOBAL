package device

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testNow = time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)

type fakeRepository struct {
	mu        sync.Mutex
	devices   map[uuid.UUID]Device
	createErr error
	getErr    error
	listErr   error
	updateErr error
	deleteErr error
}

func newFakeRepository(devices ...Device) *fakeRepository {
	repo := &fakeRepository{devices: make(map[uuid.UUID]Device, len(devices))}
	for _, d := range devices {
		repo.devices[d.ID] = d
	}
	return repo
}

func (r *fakeRepository) Create(_ context.Context, d Device) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[d.ID] = d
	return nil
}

func (r *fakeRepository) Get(_ context.Context, id uuid.UUID) (Device, error) {
	if r.getErr != nil {
		return Device{}, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	return d, nil
}

func (r *fakeRepository) List(_ context.Context, filter Filter) ([]Device, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]Device, 0, len(r.devices))
	for _, d := range r.devices {
		if filter.Brand != "" && d.Brand != filter.Brand {
			continue
		}
		if filter.State != "" && d.State != filter.State {
			continue
		}
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (r *fakeRepository) Update(_ context.Context, id uuid.UUID, mutate func(*Device) error) (Device, error) {
	if r.updateErr != nil {
		return Device{}, r.updateErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}
	if err := mutate(&d); err != nil {
		return Device{}, err
	}
	r.devices[id] = d
	return d, nil
}

func (r *fakeRepository) Delete(_ context.Context, id uuid.UUID, guard func(Device) error) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.devices[id]
	if !ok {
		return ErrNotFound
	}
	if err := guard(d); err != nil {
		return err
	}
	delete(r.devices, id)
	return nil
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []StateChangedEvent
}

func (n *fakeNotifier) Publish(event StateChangedEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
}

func (n *fakeNotifier) Events() []StateChangedEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]StateChangedEvent(nil), n.events...)
}

// testBroker implements both Notifier and Subscriber so handler tests can
// exercise the real SSE path without importing the notify package and creating
// an import cycle.
type testBroker struct {
	mu          sync.Mutex
	subscribers map[chan StateChangedEvent]struct{}
}

func newTestBroker() *testBroker {
	return &testBroker{subscribers: make(map[chan StateChangedEvent]struct{})}
}

func (b *testBroker) Publish(event StateChangedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *testBroker) Subscribe() (<-chan StateChangedEvent, func()) {
	ch := make(chan StateChangedEvent, 16)
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

func newTestService(devices ...Device) (*Service, *fakeRepository, *fakeNotifier) {
	repo := newFakeRepository(devices...)
	notifier := &fakeNotifier{}
	service := NewService(repo, notifier)
	service.now = func() time.Time { return testNow }
	return service, repo, notifier
}

func newTestHandler(t *testing.T, devices ...Device) (*http.ServeMux, *Service, *fakeRepository, *testBroker) {
	t.Helper()

	repo := newFakeRepository(devices...)
	broker := newTestBroker()
	service := NewService(repo, broker)
	service.now = func() time.Time { return testNow }

	handler := NewHandler(service, broker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	handler.Register(mux)
	return mux, service, repo, broker
}
