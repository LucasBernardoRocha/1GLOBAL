package device

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"devices-api/internal/platform"
)

const maxRequestBody = 1 << 20 // 1 MiB

// ServiceAPI is the slice of the service used by the HTTP layer. Keeping it as
// an interface makes handler tests independent from PostgreSQL.
type ServiceAPI interface {
	Create(ctx context.Context, in CreateInput) (Device, error)
	Get(ctx context.Context, id uuid.UUID) (Device, error)
	List(ctx context.Context, filter Filter) ([]Device, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (Device, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	svc    ServiceAPI
	logger *slog.Logger
}

func NewHandler(svc ServiceAPI, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/devices", h.create)
	mux.HandleFunc("GET /v1/devices", h.list)
	mux.HandleFunc("GET /v1/devices/{id}", h.get)
	mux.HandleFunc("PATCH /v1/devices/{id}", h.update)
	mux.HandleFunc("DELETE /v1/devices/{id}", h.delete)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := decodeJSON(w, r, &in); err != nil {
		platform.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	d, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/v1/devices/"+d.ID.String())
	platform.WriteJSON(w, http.StatusCreated, d)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	filter := Filter{
		Brand: r.URL.Query().Get("brand"),
		State: State(r.URL.Query().Get("state")),
	}
	if filter.State != "" && !filter.State.Valid() {
		platform.WriteError(w, http.StatusBadRequest, "validation_error", "state: must be one of: available, in-use, inactive")
		return
	}

	devices, err := h.svc.List(r.Context(), filter)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	if devices == nil {
		devices = []Device{}
	}
	platform.WriteJSON(w, http.StatusOK, devices)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	d, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	var in UpdateInput
	if err := decodeJSON(w, r, &in); err != nil {
		platform.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	d, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		platform.WriteError(w, http.StatusNotFound, "not_found", "device not found")
	case errors.Is(err, ErrInUse):
		platform.WriteError(w, http.StatusConflict, "device_in_use", "device is in use")
	case errors.Is(err, ErrImmutableCreatedAt):
		platform.WriteError(w, http.StatusBadRequest, "immutable_field", "created_at cannot be updated")
	default:
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			platform.WriteError(w, http.StatusBadRequest, "validation_error", validationErr.Error())
			return
		}

		h.logger.ErrorContext(r.Context(), "request failed",
			"error", err,
			"request_id", platform.RequestIDFromContext(r.Context()),
		)
		platform.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, &ValidationError{Field: "id", Message: "must be a valid UUID"}
	}
	return id, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return errors.New("invalid JSON body: " + err.Error())
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}
