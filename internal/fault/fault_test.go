package fault


import (
	"context"
	"errors"
	"testing"
)

func TestNewPreservesCauseAndCategory(t *testing.T) {
	cause := errors.New("state conflict")
	err := New("effect.stage", Conflict, cause)
	if !errors.Is(err, cause) || !IsCategory(err, Conflict) || CategoryOf(err) != Conflict {
		t.Fatalf("structured error lost cause or category: %v", err)
	}
}

func TestNewClassifiesCancellation(t *testing.T) {
	err := New("effect.execute", Internal, context.Canceled)
	if !IsCategory(err, Cancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}
