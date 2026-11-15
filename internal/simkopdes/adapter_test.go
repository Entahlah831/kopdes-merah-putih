package simkopdes

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestStubAdapterDeScoped(t *testing.T) {
	var adapter SyncAdapter = NewStubAdapter(nil)

	id, err := adapter.EnqueueReport(context.Background(), uuid.New(), "laporan_bulanan", map[string]interface{}{"total": 10})
	if id != uuid.Nil {
		t.Fatalf("id=%v, ingin uuid.Nil", id)
	}
	if !errors.Is(err, ErrSIMKOPDESDeScoped) {
		t.Fatalf("err=%v, ingin ErrSIMKOPDESDeScoped", err)
	}

	if err := adapter.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("ProcessQueue harus mengembalikan nil, dapat: %v", err)
	}
}
