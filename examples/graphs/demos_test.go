package main

import (
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

func TestGalleryCoversAllDemosAndRenders(t *testing.T) {
	entries := galleryEntries()
	if len(entries) != 20 {
		t.Fatalf("gallery entries = %d, want 20", len(entries))
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.name == "" || entry.category == "" || entry.description == "" || entry.features == "" || entry.build == nil {
			t.Fatalf("incomplete gallery entry: %+v", entry)
		}
		if seen[entry.name] {
			t.Fatalf("duplicate gallery entry %q", entry.name)
		}
		seen[entry.name] = true
		t.Run(entry.name, func(t *testing.T) {
			c := entry.build()
			if c == nil || len(c.Series) == 0 {
				t.Fatal("demo produced no chart series")
			}
			c.Layout(qui.Rect{W: 900, H: 620})
			_ = c.Tick(time.Now().Add(3 * time.Second))
			rec := &qui.RecordingCanvas{}
			c.Draw(rec)
			calls := len(rec.Fills) + len(rec.Rounds) + len(rec.Strokes) + len(rec.Texts) +
				len(rec.Lines) + len(rec.Polylines) + len(rec.Paths) + len(rec.Images) + len(rec.Vectors)
			if calls == 0 {
				t.Fatal("demo emitted no draw calls")
			}
		})
	}
}
