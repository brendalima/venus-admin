package services

import (
	"context"
	"errors"
	"testing"
)

type mockHealthRepository struct{ err error }

func (m mockHealthRepository) Ping(context.Context) error { return m.err }

func TestNewHealth(t *testing.T) {
	if _, err := NewHealth(nil); err == nil {
		t.Error("a nil repository should be rejected")
	}
}

func TestHealthReady(t *testing.T) {
	dbDown := errors.New("timeout")

	tests := []struct {
		name    string
		repoErr error
		wantErr error
	}{
		{"database answering", nil, nil},
		{"database down", dbDown, dbDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := MustNewHealth(mockHealthRepository{err: tt.repoErr})

			err := h.Ready(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Ready() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
