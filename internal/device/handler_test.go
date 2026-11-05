package device

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func doRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) apiError {
	t.Helper()

	var body apiError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return body
}

func TestHandlerCreate(t *testing.T) {
	mux, _, repo, _ := newTestHandler(t)

	rec := doRequest(t, mux, http.MethodPost, "/v1/devices", `{"name":"iPhone 15","brand":"Apple"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	var created Device
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("created device has no ID")
	}
	if created.State != StateAvailable {
		t.Fatalf("state = %q, want %q", created.State, StateAvailable)
	}
	if got, want := rec.Header().Get("Location"), "/v1/devices/"+created.ID.String(); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}

	stored, err := repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("created device was not persisted: %v", err)
	}
	if stored != created {
		t.Fatalf("stored device = %+v, want %+v", stored, created)
	}
}

func TestHandlerCreateRejectsInvalidBodies(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{name: "malformed json", body: `{`, wantCode: "invalid_request"},
		{name: "empty body", body: "", wantCode: "invalid_request"},
		{name: "unknown field", body: `{"name":"iPhone 15","brand":"Apple","created_at":"2025-01-02T03:04:05Z"}`, wantCode: "invalid_request"},
		{name: "missing name", body: `{"brand":"Apple"}`, wantCode: "validation_error"},
		{name: "invalid state", body: `{"name":"iPhone 15","brand":"Apple","state":"lost"}`, wantCode: "validation_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _, _, _ := newTestHandler(t)
			rec := doRequest(t, mux, http.MethodPost, "/v1/devices", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := decodeError(t, rec).Error.Code; got != tt.wantCode {
				t.Fatalf("error code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestHandlerList(t *testing.T) {
	appleAvailable := Device{
		ID: uuid.New(), Name: "iPhone 15", Brand: "Apple",
		State: StateAvailable, CreatedAt: testNow.Add(-time.Hour),
	}
	appleInUse := Device{
		ID: uuid.New(), Name: "iPad Pro", Brand: "Apple",
		State: StateInUse, CreatedAt: testNow.Add(-2 * time.Hour),
	}
	samsung := Device{
		ID: uuid.New(), Name: "Galaxy S24", Brand: "Samsung",
		State: StateAvailable, CreatedAt: testNow.Add(-3 * time.Hour),
	}
	mux, _, _, _ := newTestHandler(t, appleAvailable, appleInUse, samsung)

	tests := []struct {
		name     string
		target   string
		want     int
		wantCode string
	}{
		{name: "all", target: "/v1/devices", want: 3},
		{name: "by brand", target: "/v1/devices?brand=Apple", want: 2},
		{name: "by state", target: "/v1/devices?state=in-use", want: 1},
		{name: "combined", target: "/v1/devices?brand=Apple&state=available", want: 1},
		{name: "invalid state", target: "/v1/devices?state=lost", wantCode: "validation_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, mux, http.MethodGet, tt.target, "")
			if tt.wantCode != "" {
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
				}
				if got := decodeError(t, rec).Error.Code; got != tt.wantCode {
					t.Fatalf("error code = %q, want %q", got, tt.wantCode)
				}
				return
			}

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			var devices []Device
			if err := json.NewDecoder(rec.Body).Decode(&devices); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(devices) != tt.want {
				t.Fatalf("len(devices) = %d, want %d", len(devices), tt.want)
			}
		})
	}
}

func TestHandlerGet(t *testing.T) {
	want := Device{
		ID: uuid.New(), Name: "iPhone 15", Brand: "Apple",
		State: StateAvailable, CreatedAt: testNow,
	}
	mux, _, _, _ := newTestHandler(t, want)

	rec := doRequest(t, mux, http.MethodGet, "/v1/devices/"+want.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got Device
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got != want {
		t.Fatalf("response = %+v, want %+v", got, want)
	}

	rec = doRequest(t, mux, http.MethodGet, "/v1/devices/not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := decodeError(t, rec).Error.Code; got != "validation_error" {
		t.Fatalf("invalid id error code = %q, want validation_error", got)
	}

	rec = doRequest(t, mux, http.MethodGet, "/v1/devices/"+uuid.NewString(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing device status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeError(t, rec).Error.Code; got != "not_found" {
		t.Fatalf("missing device error code = %q, want not_found", got)
	}
}

func TestHandlerUpdate(t *testing.T) {
	t.Run("updates a device", func(t *testing.T) {
		id := uuid.New()
		mux, _, repo, _ := newTestHandler(t, Device{
			ID: id, Name: "Old name", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodPatch, "/v1/devices/"+id.String(), `{"name":"New name","state":"in-use"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var updated Device
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if updated.Name != "New name" || updated.State != StateInUse {
			t.Fatalf("response = %+v, want updated fields", updated)
		}

		stored, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("stored device not found: %v", err)
		}
		if stored != updated {
			t.Fatalf("stored device = %+v, want %+v", stored, updated)
		}
	})

	t.Run("rejects created_at update", func(t *testing.T) {
		id := uuid.New()
		mux, _, _, _ := newTestHandler(t, Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodPatch, "/v1/devices/"+id.String(), `{"created_at":"2025-01-03T03:04:05Z"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := decodeError(t, rec).Error.Code; got != "immutable_field" {
			t.Fatalf("error code = %q, want immutable_field", got)
		}
	})

	t.Run("rejects name update while in use", func(t *testing.T) {
		id := uuid.New()
		mux, _, _, _ := newTestHandler(t, Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateInUse, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodPatch, "/v1/devices/"+id.String(), `{"name":"New name"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
		}
		if got := decodeError(t, rec).Error.Code; got != "device_in_use" {
			t.Fatalf("error code = %q, want device_in_use", got)
		}
	})

	t.Run("rejects invalid state", func(t *testing.T) {
		id := uuid.New()
		mux, _, _, _ := newTestHandler(t, Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodPatch, "/v1/devices/"+id.String(), `{"state":"lost"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := decodeError(t, rec).Error.Code; got != "validation_error" {
			t.Fatalf("error code = %q, want validation_error", got)
		}
	})

	t.Run("missing device", func(t *testing.T) {
		mux, _, _, _ := newTestHandler(t)
		rec := doRequest(t, mux, http.MethodPatch, "/v1/devices/"+uuid.NewString(), `{"name":"New name"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandlerDelete(t *testing.T) {
	t.Run("deletes an available device", func(t *testing.T) {
		id := uuid.New()
		mux, _, repo, _ := newTestHandler(t, Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateAvailable, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodDelete, "/v1/devices/"+id.String(), "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("body = %q, want empty", rec.Body.String())
		}
		if _, err := repo.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("device still exists after delete: %v", err)
		}
	})

	t.Run("rejects an in-use device", func(t *testing.T) {
		id := uuid.New()
		mux, _, repo, _ := newTestHandler(t, Device{
			ID: id, Name: "iPhone 15", Brand: "Apple",
			State: StateInUse, CreatedAt: testNow,
		})

		rec := doRequest(t, mux, http.MethodDelete, "/v1/devices/"+id.String(), "")
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
		}
		if got := decodeError(t, rec).Error.Code; got != "device_in_use" {
			t.Fatalf("error code = %q, want device_in_use", got)
		}
		if _, err := repo.Get(context.Background(), id); err != nil {
			t.Fatalf("in-use device was removed: %v", err)
		}
	})

	t.Run("missing device", func(t *testing.T) {
		mux, _, _, _ := newTestHandler(t)
		rec := doRequest(t, mux, http.MethodDelete, "/v1/devices/"+uuid.NewString(), "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandlerStreamsStateChanges(t *testing.T) {
	id := uuid.New()
	mux, service, _, _ := newTestHandler(t, Device{
		ID: id, Name: "iPhone 15", Brand: "Apple",
		State: StateAvailable, CreatedAt: testNow,
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(server.URL + "/v1/devices/events")
	if err != nil {
		t.Fatalf("connect to event stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read stream header: %v", err)
	}
	if firstLine != ": connected\n" {
		t.Fatalf("first line = %q, want %q", firstLine, ": connected\n")
	}

	state := StateInUse
	if _, err := service.Update(context.Background(), id, UpdateInput{State: &state}); err != nil {
		t.Fatalf("update device: %v", err)
	}

	var data string
	for i := 0; i < 5; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event line: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			break
		}
	}
	if data == "" {
		t.Fatal("event stream did not include a data line")
	}

	var event StateChangedEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.DeviceID != id || event.PreviousState != StateAvailable || event.NewState != StateInUse {
		t.Fatalf("event = %+v, want available -> in-use for %s", event, id)
	}
	if event.ChangedAt.IsZero() {
		t.Fatal("event has no changed_at timestamp")
	}
}
