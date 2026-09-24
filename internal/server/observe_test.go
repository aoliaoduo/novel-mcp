package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestObserverCounts(t *testing.T) {
	o := NewObserver()
	o.Record("plan_chapter", "p1", true, time.Millisecond, "")
	o.Record("draft_chapter", "p1", false, 2*time.Millisecond, "boom")
	c, s, f := o.Stats.Snapshot()
	if c != 2 || s != 1 || f != 1 {
		t.Fatalf("got %d/%d/%d", c, s, f)
	}
	evs := o.Log.Recent(10)
	if len(evs) != 2 || evs[0].Tool != "draft_chapter" || evs[0].OK || evs[1].Tool != "plan_chapter" {
		t.Fatalf("order/content wrong: %+v", evs)
	}
	if evs[0].Err != "boom" || evs[1].Err != "" {
		t.Fatalf("err field wrong: %+v", evs)
	}
}

func TestEventLogRing(t *testing.T) {
	l := NewEventLog(3)
	for i := 0; i < 5; i++ {
		l.Add(Event{Tool: fmt.Sprintf("t%d", i)})
	}
	got := l.Recent(0)
	if len(got) != 3 || got[0].Tool != "t4" || got[1].Tool != "t3" || got[2].Tool != "t2" {
		t.Fatalf("ring wrong: %+v", got)
	}
	if one := l.Recent(1); len(one) != 1 || one[0].Tool != "t4" {
		t.Fatalf("limit wrong: %+v", one)
	}
}

func TestObserverNilSafe(t *testing.T) {
	var o *Observer
	o.Record("x", "y", true, 0, "") // must not panic
	var l *EventLog
	l.Add(Event{})
	if l.Recent(5) != nil {
		t.Fatal("nil log should return nil")
	}
	zero := &Observer{}
	zero.Record("x", "y", false, 0, "e") // Log nil: count only
	if c, _, f := zero.Stats.Snapshot(); c != 1 || f != 1 {
		t.Fatal("zero observer should count")
	}
}

func TestObserverConcurrent(t *testing.T) {
	o := NewObserver()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				o.Record("t", "p", i%2 == 0, time.Microsecond, "")
			}
		}()
	}
	wg.Wait()
	if c, s, f := o.Stats.Snapshot(); c != 1600 || s != 800 || f != 800 {
		t.Fatalf("got %d/%d/%d", c, s, f)
	}
}

func TestShortErr(t *testing.T) {
	if got := shortErr("a\n  b\tc"); got != "a b c" {
		t.Fatalf("got %q", got)
	}
	long := ""
	for i := 0; i < 200; i++ {
		long += "字"
	}
	if got := shortErr(long); len([]rune(got)) != 121 {
		t.Fatalf("want 120+… got %d runes", len([]rune(got)))
	}
}
