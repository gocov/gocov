package main

import (
	"testing"
	"time"
)

// The pool line reports the gauges as they are and the counters as what
// moved since the last line, and says nothing for a minute with no use.
func TestPoolStatsAttrs(t *testing.T) {
	prev := poolSample{max: 4, total: 2, idle: 2, acquires: 100, emptyAcquires: 5, emptyAcquireWait: 2 * time.Second, canceledAcquires: 1}

	if _, ok := poolStatsAttrs(prev, prev); ok {
		t.Error("an idle minute was logged")
	}

	cur := poolSample{max: 4, total: 4, idle: 1, acquired: 3, acquires: 160, emptyAcquires: 8, emptyAcquireWait: 3500 * time.Millisecond, canceledAcquires: 1}
	attrs, ok := poolStatsAttrs(prev, cur)
	if !ok {
		t.Fatal("a busy minute was not logged")
	}
	got := map[string]any{}
	for i := 0; i < len(attrs); i += 2 {
		got[attrs[i].(string)] = attrs[i+1]
	}
	want := map[string]any{
		"acquired": int32(3), "idle": int32(1), "total": int32(4), "max": int32(4),
		"acquires": int64(60), "empty_acquires": int64(3), "empty_acquire_wait_ms": int64(1500), "canceled_acquires": int64(0),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got[k], got[k], v, v)
		}
	}

	// A connection held across the whole minute is still worth a line,
	// though nothing new was acquired: it is what a stuck push looks like.
	held := prev
	held.acquired = 1
	if _, ok := poolStatsAttrs(prev, held); !ok {
		t.Error("a minute with a connection held throughout was not logged")
	}
}
