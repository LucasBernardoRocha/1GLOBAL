package device

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStateValid(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  bool
	}{
		{name: "available", state: StateAvailable, want: true},
		{name: "in use", state: StateInUse, want: true},
		{name: "inactive", state: StateInactive, want: true},
		{name: "empty", state: "", want: false},
		{name: "unknown", state: "broken", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Valid(); got != tt.want {
				t.Fatalf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateInputValidate(t *testing.T) {
	tests := []struct {
		name      string
		input     CreateInput
		wantField string
		wantErr   error
	}{
		{
			name:  "valid with explicit state",
			input: CreateInput{Name: "iPhone 15", Brand: "Apple", State: StateAvailable},
		},
		{
			name:  "valid after default state normalization",
			input: CreateInput{Name: "iPhone 15", Brand: "Apple"},
		},
		{
			name:      "missing name",
			input:     CreateInput{Name: "   ", Brand: "Apple"},
			wantField: "name",
		},
		{
			name:      "missing brand",
			input:     CreateInput{Name: "iPhone 15", Brand: " "},
			wantField: "brand",
		},
		{
			name:      "name too long",
			input:     CreateInput{Name: strings.Repeat("a", maxNameLength+1), Brand: "Apple"},
			wantField: "name",
		},
		{
			name:      "brand too long",
			input:     CreateInput{Name: "iPhone 15", Brand: strings.Repeat("a", maxBrandLength+1)},
			wantField: "brand",
		},
		{
			name:      "invalid state",
			input:     CreateInput{Name: "iPhone 15", Brand: "Apple", State: "lost"},
			wantField: "state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			in.Normalize()
			err := in.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				if in.State != StateAvailable && tt.input.State == "" {
					t.Fatalf("Normalize() state = %q, want %q", in.State, StateAvailable)
				}
				return
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Validate() error = %v, want *ValidationError", err)
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("ValidationError.Field = %q, want %q", validationErr.Field, tt.wantField)
			}
		})
	}
}

func TestUpdateInputValidate(t *testing.T) {
	name := "iPhone 15"
	empty := " "
	state := StateInUse
	invalidState := State("lost")
	createdAt := time.Now()

	tests := []struct {
		name      string
		input     UpdateInput
		wantField string
		wantErr   error
	}{
		{name: "empty update is valid", input: UpdateInput{}},
		{name: "valid partial update", input: UpdateInput{Name: &name, State: &state}},
		{
			name:    "created_at is immutable",
			input:   UpdateInput{CreatedAt: &createdAt},
			wantErr: ErrImmutableCreatedAt,
		},
		{
			name:      "empty name",
			input:     UpdateInput{Name: &empty},
			wantField: "name",
		},
		{
			name:      "invalid state",
			input:     UpdateInput{State: &invalidState},
			wantField: "state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			in.Normalize()
			err := in.Validate()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Validate() error = %v, want *ValidationError", err)
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("ValidationError.Field = %q, want %q", validationErr.Field, tt.wantField)
			}
		})
	}
}
