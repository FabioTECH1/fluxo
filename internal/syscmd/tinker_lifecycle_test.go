package syscmd

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTinkerCleanupRequiresPositiveEmptyPopulation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitTinkerPopulation(ctx, func() ([]byte, error) { return []byte("populated 1\nfrozen 0\n"), nil }); err == nil {
		t.Fatal("a surviving domain was treated as drained")
	}
	if err := waitTinkerPopulation(context.Background(), func() ([]byte, error) { return []byte("populated 0\nfrozen 0\n"), nil }); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"frozen 0\n", "populated 2\n", "populated 0\npopulated 1\n", "populated\n"} {
		if err := waitTinkerPopulation(context.Background(), func() ([]byte, error) { return []byte(raw), nil }); err == nil {
			t.Fatal("invalid population proof was accepted", raw)
		}
	}
	sentinel := errors.New("unreadable domain")
	if err := waitTinkerPopulation(context.Background(), func() ([]byte, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("observation failure lost uncertainty", err)
	}
}
