package qui

import "testing"

// Work a job defers with BeforeLayout lands in the same frame, before
// layout — where a PostJob from inside the job would wait a frame.
func TestBeforeLayoutRunsInTheSameFrame(t *testing.T) {
	lockUIThreadForTest(t)
	w, err := NewHeadlessWindowForTest(200, 100)
	if err != nil {
		t.Skip(err)
	}
	ran, posted := false, false
	w.PostJob(func() {
		w.BeforeLayout(func() { ran = true })
		w.PostJob(func() { posted = true })
	})
	w.Step()
	if !ran {
		t.Fatal("BeforeLayout callback did not run in the frame of the job that queued it")
	}
	if posted {
		t.Fatal("a job posted from a job should wait for the next frame")
	}
	w.Step()
	if !posted {
		t.Fatal("posted job never ran")
	}
}
