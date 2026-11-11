package qui

import "testing"

type fakeMonitor struct{ hz int }

func (fakeMonitor) name() string                     { return "Fake" }
func (fakeMonitor) position() (int, int)             { return 0, 0 }
func (m fakeMonitor) videoMode() (int, int, int)     { return 3024, 1964, m.hz }
func (fakeMonitor) workArea() (int, int, int, int)   { return 0, 0, 1512, 945 }
func (fakeMonitor) contentScale() (float32, float32) { return 2, 2 }

func TestMonitorReportsRefreshRate(t *testing.T) {
	if m := monitorFrom(fakeMonitor{120}); m.RefreshRate != 120 {
		t.Fatalf("refresh %d", m.RefreshRate)
	}
	if m := monitorFrom(fakeMonitor{-1}); m.RefreshRate != 0 {
		t.Fatalf("unknown refresh reported as %d", m.RefreshRate)
	}
}
