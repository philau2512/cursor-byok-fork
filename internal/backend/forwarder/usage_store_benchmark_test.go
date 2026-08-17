package forwarder

import (
	"context"
	"testing"
	"time"
)

func BenchmarkUsageFileStoreUpsertEvent(b *testing.B) {
	store := NewUsageFileStore(b.TempDir())
	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			b.Error(err)
		}
	})
	event := testUsageEvent("benchmark-event", 100)

	b.ReportAllocs()
	for b.Loop() {
		if err := store.UpsertEvent(event); err != nil {
			b.Fatal(err)
		}
	}
}
