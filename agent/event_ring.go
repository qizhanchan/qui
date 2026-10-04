package agent

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// EventRecord is one entry in the agent's event log — typically a
// dispatched event, but the recorder also pushes layout/repaint/focus
// transitions. Each record carries a monotonic SeqID so consumers
// can de-duplicate across /console + SSE.
type EventRecord struct {
	SeqID     int64  `json:"seq"`
	Timestamp int64  `json:"ts"` // unix nanos
	Kind      string `json:"kind"`
	Target    string `json:"target,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// eventRing is a bounded ring buffer of EventRecord plus a fan-out
// channel for SSE subscribers. Producers call Push; consumers call
// Subscribe to receive a live channel that closes on Unsubscribe.
//
// The ring buffer is held under a single mutex (a slice with head /
// count, not a circular indexing scheme — simpler and still O(1)
// per push since N is small).
type eventRing struct {
	mu      sync.Mutex
	cap     int
	items   []EventRecord
	subs    map[chan EventRecord]struct{}
	seqNext atomic.Int64
}

func newEventRing(capacity int) *eventRing {
	if capacity <= 0 {
		capacity = 256
	}
	return &eventRing{
		cap:   capacity,
		items: make([]EventRecord, 0, capacity),
		subs:  make(map[chan EventRecord]struct{}),
	}
}

// Push appends a new record (allocating a SeqID atomically) and
// notifies subscribers. Drops the oldest item if capacity is
// reached.
func (r *eventRing) Push(rec EventRecord) {
	rec.SeqID = r.seqNext.Add(1)
	r.mu.Lock()
	if len(r.items) >= r.cap {
		copy(r.items, r.items[1:])
		r.items = r.items[:len(r.items)-1]
	}
	r.items = append(r.items, rec)
	subs := make([]chan EventRecord, 0, len(r.subs))
	for ch := range r.subs {
		subs = append(subs, ch)
	}
	r.mu.Unlock()
	// Non-blocking fan-out: a slow consumer that's full just drops
	// the record. SSE clients reconcile via /console?since=… on
	// reconnect.
	for _, ch := range subs {
		select {
		case ch <- rec:
		default:
		}
	}
}

// All returns a copy of every record currently buffered.
func (r *eventRing) All() []EventRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventRecord, len(r.items))
	copy(out, r.items)
	return out
}

// Since returns records strictly newer than the given SeqID string
// (decimal). An empty / unparseable since returns everything.
func (r *eventRing) Since(since string) []EventRecord {
	threshold := int64(-1)
	if since != "" {
		if v, err := strconv.ParseInt(since, 10, 64); err == nil {
			threshold = v
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventRecord, 0)
	for _, rec := range r.items {
		if rec.SeqID > threshold {
			out = append(out, rec)
		}
	}
	return out
}

// Subscribe returns a channel that receives new records. Buffered
// at 32 to absorb micro-bursts without forcing the producer to
// drop. Unsubscribe closes it.
func (r *eventRing) Subscribe() chan EventRecord {
	ch := make(chan EventRecord, 32)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

func (r *eventRing) Unsubscribe(ch chan EventRecord) {
	r.mu.Lock()
	if _, ok := r.subs[ch]; ok {
		delete(r.subs, ch)
		close(ch)
	}
	r.mu.Unlock()
}

// Record exposes Push for the recording layer to push
// new entries without reaching into private fields.
func (s *Server) Record(rec EventRecord) {
	if s == nil || s.recordedEvents == nil {
		return
	}
	s.recordedEvents.Push(rec)
}
