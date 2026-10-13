package common

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ErrNotFound", ErrNotFound, true},
		{"gorm record not found", gorm.ErrRecordNotFound, true},
		{"wrapped ErrNotFound", fmt.Errorf("ctx: %w", ErrNotFound), true},
		{"wrapped gorm", fmt.Errorf("ctx: %w", gorm.ErrRecordNotFound), true},
		{"joined", errors.Join(errors.New("a"), gorm.ErrRecordNotFound), true},
		{"other", errors.New("boom"), false},
		{"forbidden", ErrForbidden, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Fatalf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
