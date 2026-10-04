package widgets

// singleSelectionModel is the shared single-index selection state used by
// fixed-row views. It intentionally stays package-private for now so ListView,
// TableView, and TreeView can share selection semantics without changing their
// public SelectedIdx fields.
type singleSelectionModel struct {
	index int
}

func newSingleSelectionModel() singleSelectionModel {
	return singleSelectionModel{index: -1}
}

func (m *singleSelectionModel) Sync(index int) {
	m.index = index
}

func (m *singleSelectionModel) Index() int {
	return m.index
}

func (m *singleSelectionModel) Reset() bool {
	if m.index == -1 {
		return false
	}
	m.index = -1
	return true
}

func (m *singleSelectionModel) Clamp(count int) bool {
	next := m.index
	switch {
	case count <= 0:
		next = -1
	case next < -1:
		next = -1
	case next >= count:
		next = count - 1
	}
	if next == m.index {
		return false
	}
	m.index = next
	return true
}

func (m *singleSelectionModel) Select(index, count int) (int, bool) {
	if count <= 0 {
		changed := m.Reset()
		return m.index, changed
	}
	if index < 0 {
		index = 0
	}
	if index >= count {
		index = count - 1
	}
	if index == m.index {
		return m.index, false
	}
	m.index = index
	return m.index, true
}
