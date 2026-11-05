package notify

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"devices-api/internal/device"
)

func TestBrokerPublishesToAllSubscribers(t *testing.T) {
	broker := NewBroker()
	first, cancelFirst := broker.Subscribe()
	defer cancelFirst()
	second, cancelSecond := broker.Subscribe()
	defer cancelSecond()

	event := device.StateChangedEvent{
		DeviceID:      uuid.New(),
		PreviousState: device.StateAvailable,
		NewState:      device.StateInUse,
		ChangedAt:     time.Now().UTC(),
	}
	broker.Publish(event)

	for i, channel := range []<-chan device.StateChangedEvent{first, second} {
		select {
		case got := <-channel:
			if got != event {
				t.Fatalf("subscriber %d got %+v, want %+v", i, got, event)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive the event", i)
		}
	}
}

func TestBrokerUnsubscribeIsIdempotentAndClosesChannel(t *testing.T) {
	broker := NewBroker()
	channel, cancel := broker.Subscribe()

	cancel()
	cancel()

	select {
	case _, open := <-channel:
		if open {
			t.Fatal("channel should be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed")
	}
}

func TestBrokerDoesNotBlockOnSlowSubscriber(t *testing.T) {
	broker := NewBroker()
	_, cancel := broker.Subscribe()
	defer cancel()

	event := device.StateChangedEvent{
		DeviceID:      uuid.New(),
		PreviousState: device.StateAvailable,
		NewState:      device.StateInactive,
		ChangedAt:     time.Now().UTC(),
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			broker.Publish(event)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
