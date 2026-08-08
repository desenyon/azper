package identity

import (
	"strings"
	"testing"
	"time"
)

func TestNewProducesPrefixedSortableIDs(t *testing.T) {
	first, err := New("ctr", time.UnixMilli(1_000))
	if err != nil {
		t.Fatal(err)
	}
	second, err := New("ctr", time.UnixMilli(2_000))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(first, "ctr_0000000001000_") {
		t.Fatalf("unexpected identifier %q", first)
	}
	if first >= second {
		t.Fatalf("expected %q to sort before %q", first, second)
	}
}

func TestNewRejectsInvalidPrefix(t *testing.T) {
	if _, err := New("Contract", time.Now()); err == nil {
		t.Fatal("expected invalid prefix error")
	}
}
