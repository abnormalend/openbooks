// server/api/jobs_test.go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/core"
)

func TestEnqueueReturnsPositionAndQueueFull(t *testing.T) {
	r := NewRegistry(2, time.Hour)
	for i := 1; i <= 2; i++ {
		pos, err := r.Enqueue(NewSearchJob("q", 0))
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		if pos != i {
			t.Errorf("position = %d, want %d", pos, i)
		}
	}
	if _, err := r.Enqueue(NewSearchJob("q", 0)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third enqueue err = %v, want ErrQueueFull", err)
	}
	// Downloads have their own queue.
	if _, err := r.Enqueue(NewDownloadJob("!x book.epub")); err != nil {
		t.Fatalf("download enqueue: %v", err)
	}
}

func TestNextMarksRunningAndCounts(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	job := NewSearchJob("q", 0)
	r.Enqueue(job)

	got := r.Next(context.Background(), JobSearch)
	if got != job {
		t.Fatal("Next returned a different job")
	}
	snap, _ := r.Get(job.ID)
	if snap.Status != StatusRunning || snap.StartedAt == nil {
		t.Errorf("after Next: %+v", snap)
	}
	running, queued := r.Counts(JobSearch)
	if !running || queued != 0 {
		t.Errorf("Counts = (%v,%d), want (true,0)", running, queued)
	}
	if !r.Busy() {
		t.Error("Busy should be true while a job runs")
	}
}

func TestNextReturnsNilOnCancel(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := r.Next(ctx, JobSearch); got != nil {
		t.Fatal("expected nil on cancelled ctx")
	}
}

func TestFinishAndUpdate(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	job := NewSearchJob("q", 0)
	r.Enqueue(job)
	r.Next(context.Background(), JobSearch)

	r.Update(job, func(j *Job) { j.Results = []core.BookDetail{{Title: "T"}} })
	r.Finish(job, nil)

	snap, ok := r.Get(job.ID)
	if !ok || snap.Status != StatusComplete || snap.FinishedAt == nil || len(snap.Results) != 1 {
		t.Errorf("after Finish: %+v", snap)
	}
	if r.Busy() {
		t.Error("Busy should be false after Finish")
	}

	job2 := NewDownloadJob("!x")
	r.Enqueue(job2)
	r.Next(context.Background(), JobDownload)
	r.Finish(job2, &APIError{Code: "timeout", Message: "no reply"})
	snap2, _ := r.Get(job2.ID)
	if snap2.Status != StatusError || snap2.Error == nil || snap2.Error.Code != "timeout" {
		t.Errorf("after error Finish: %+v", snap2)
	}
}

func TestListNewestFirstAndFilter(t *testing.T) {
	r := NewRegistry(5, time.Hour)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { now = now.Add(time.Second); return now }
	a := NewSearchJob("a", 0)
	b := NewDownloadJob("!b")
	c := NewSearchJob("c", 0)
	r.Enqueue(a)
	r.Enqueue(b)
	r.Enqueue(c)

	all := r.List("")
	if len(all) != 3 || all[0].ID != c.ID || all[2].ID != a.ID {
		t.Errorf("List(\"\") order wrong: %+v", all)
	}
	only := r.List(JobDownload)
	if len(only) != 1 || only[0].ID != b.ID {
		t.Errorf("List(download) = %+v", only)
	}
}

func TestSweepDropsOldFinishedJobsOnly(t *testing.T) {
	r := NewRegistry(5, time.Minute)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }

	old := NewSearchJob("old", 0)
	r.Enqueue(old)
	r.Next(context.Background(), JobSearch)
	r.Finish(old, nil)

	fresh := NewSearchJob("fresh", 0) // still queued, must survive
	r.Enqueue(fresh)

	now = now.Add(2 * time.Minute)
	r.Sweep()

	if _, ok := r.Get(old.ID); ok {
		t.Error("old finished job should be swept")
	}
	if _, ok := r.Get(fresh.ID); !ok {
		t.Error("queued job must not be swept")
	}
}

func TestJobJSONShapePerType(t *testing.T) {
	s := NewSearchJob("q", 5)
	b, _ := json.Marshal(s)
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	for _, k := range []string{"jobId", "type", "status", "createdAt", "startedAt", "finishedAt", "error", "query", "limit", "results", "parseErrors"} {
		if _, ok := m[k]; !ok {
			t.Errorf("search json missing %q: %s", k, b)
		}
	}
	if _, ok := m["book"]; ok {
		t.Errorf("search json must not carry download fields: %s", b)
	}
	if m["results"] != nil {
		t.Errorf("queued search results should be null, got %v", m["results"])
	}

	d := NewDownloadJob("!x")
	b, _ = json.Marshal(d)
	m = map[string]interface{}{}
	json.Unmarshal(b, &m)
	for _, k := range []string{"jobId", "type", "status", "book", "bytes", "size", "path", "fileName", "error"} {
		if _, ok := m[k]; !ok {
			t.Errorf("download json missing %q: %s", k, b)
		}
	}
	if _, ok := m["query"]; ok {
		t.Errorf("download json must not carry search fields: %s", b)
	}
}

func TestBusyIsTrueFromEnqueueUntilFinish(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	job := NewSearchJob("q", 0)
	r.Enqueue(job)

	if !r.Busy() {
		t.Error("Busy should be true right after Enqueue")
	}
	if running, queued := r.Counts(JobSearch); running || queued != 1 {
		t.Errorf("Counts after Enqueue = (%v,%d), want (false,1)", running, queued)
	}

	r.Next(context.Background(), JobSearch)
	if !r.Busy() {
		t.Error("Busy should be true after Next")
	}
	if running, queued := r.Counts(JobSearch); !running || queued != 0 {
		t.Errorf("Counts after Next = (%v,%d), want (true,0)", running, queued)
	}

	r.Finish(job, nil)
	if r.Busy() {
		t.Error("Busy should be false after Finish")
	}
}
