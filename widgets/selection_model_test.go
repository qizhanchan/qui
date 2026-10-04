package widgets

import "testing"

func TestSingleSelectionModelSelectClampsAndReportsChange(t *testing.T) {
	m := newSingleSelectionModel()
	idx, changed := m.Select(99, 3)
	if idx != 2 || !changed {
		t.Fatalf("Select above range = (%d,%v), want (2,true)", idx, changed)
	}
	idx, changed = m.Select(2, 3)
	if idx != 2 || changed {
		t.Fatalf("Select same = (%d,%v), want (2,false)", idx, changed)
	}
	idx, changed = m.Select(-10, 3)
	if idx != 0 || !changed {
		t.Fatalf("Select below range = (%d,%v), want (0,true)", idx, changed)
	}
}

func TestSingleSelectionModelEmptySelectionClears(t *testing.T) {
	m := newSingleSelectionModel()
	m.Sync(4)
	idx, changed := m.Select(0, 0)
	if idx != -1 || !changed {
		t.Fatalf("Select empty = (%d,%v), want (-1,true)", idx, changed)
	}
	idx, changed = m.Select(0, 0)
	if idx != -1 || changed {
		t.Fatalf("Select empty again = (%d,%v), want (-1,false)", idx, changed)
	}
}

func TestSingleSelectionModelClampPreservesValidIndex(t *testing.T) {
	m := newSingleSelectionModel()
	m.Sync(1)
	if changed := m.Clamp(3); changed {
		t.Fatalf("Clamp valid index changed selection")
	}
	if got := m.Index(); got != 1 {
		t.Fatalf("Clamp valid index = %d, want 1", got)
	}
	if changed := m.Clamp(1); !changed {
		t.Fatalf("Clamp to smaller count should report change")
	}
	if got := m.Index(); got != 0 {
		t.Fatalf("Clamp smaller count = %d, want 0", got)
	}
}
