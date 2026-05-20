package services

import (
	"testing"
	"time"
)

func TestAIJobStoreGetReturnsCopy(t *testing.T) {
	store := &AIJobStore{jobs: make(map[string]*AIJob)}
	store.Create("job-1")

	job, ok := store.Get("job-1")
	if !ok {
		t.Fatal("expected job to exist")
	}

	job.Status = JobDone
	job.Content = "mutated outside store"

	stored, ok := store.Get("job-1")
	if !ok {
		t.Fatal("expected job to still exist")
	}
	if stored.Status != JobPending {
		t.Fatalf("expected stored status to remain pending, got %s", stored.Status)
	}
	if stored.Content != "" {
		t.Fatalf("expected stored content to remain empty, got %q", stored.Content)
	}
}

func TestAIJobStoreTerminalJobExpiresFromUpdatedAt(t *testing.T) {
	now := time.Now()
	store := &AIJobStore{jobs: map[string]*AIJob{
		"fresh": {
			ID:        "fresh",
			Status:    JobDone,
			Content:   "ok",
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now,
		},
		"expired": {
			ID:        "expired",
			Status:    JobDone,
			Content:   "old",
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-jobTTL - time.Second),
		},
	}}

	if _, ok := store.Get("fresh"); !ok {
		t.Fatal("expected fresh completed job to be retained")
	}

	if _, ok := store.Get("expired"); ok {
		t.Fatal("expected expired completed job to be removed")
	}
}

func TestAIJobStorePruneUsesPendingAndTerminalTTL(t *testing.T) {
	now := time.Now()
	store := &AIJobStore{jobs: map[string]*AIJob{
		"pending-old": {
			ID:        "pending-old",
			Status:    JobPending,
			CreatedAt: now.Add(-pendingJobTTL - time.Second),
			UpdatedAt: now.Add(-pendingJobTTL - time.Second),
		},
		"pending-fresh": {
			ID:        "pending-fresh",
			Status:    JobPending,
			CreatedAt: now.Add(-time.Minute),
			UpdatedAt: now.Add(-time.Minute),
		},
		"done-old": {
			ID:        "done-old",
			Status:    JobDone,
			Content:   "old",
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-jobTTL - time.Second),
		},
		"done-fresh": {
			ID:        "done-fresh",
			Status:    JobDone,
			Content:   "fresh",
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now,
		},
	}}

	store.Prune()

	if _, ok := store.jobs["pending-old"]; ok {
		t.Fatal("expected old pending job to be pruned")
	}
	if _, ok := store.jobs["done-old"]; ok {
		t.Fatal("expected old completed job to be pruned")
	}
	if _, ok := store.jobs["pending-fresh"]; !ok {
		t.Fatal("expected fresh pending job to be retained")
	}
	if _, ok := store.jobs["done-fresh"]; !ok {
		t.Fatal("expected fresh completed job to be retained")
	}
}
