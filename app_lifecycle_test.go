package qui

import (
	"errors"
	"testing"
)

func TestNewAppRejectsSecondProcessApp(t *testing.T) {
	appMu.Lock()
	previous := processApp
	processApp = &App{}
	appMu.Unlock()
	defer func() {
		appMu.Lock()
		processApp = previous
		appMu.Unlock()
	}()

	app, err := NewApp()
	if app != nil {
		t.Fatalf("NewApp returned duplicate App %p", app)
	}
	if !errors.Is(err, ErrAppAlreadyExists) {
		t.Fatalf("NewApp error = %v, want ErrAppAlreadyExists", err)
	}
}

func TestAppPrunesDestroyedWindows(t *testing.T) {
	a := &App{}
	alive := newWindowOnFake(newFakePlatformWindow())
	closed := newWindowOnFake(newFakePlatformWindow())
	closed.Destroy()
	a.windows = []*Window{closed, nil, alive}

	a.pruneDestroyedWindows()

	if len(a.windows) != 1 || a.windows[0] != alive {
		t.Fatalf("windows after prune = %v, want only live window", a.windows)
	}
}
