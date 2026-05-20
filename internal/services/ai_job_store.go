package services

import (
	"sync"
	"time"
)

// JobStatus represents the lifecycle state of an async AI job.
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"

	// jobTTL is how long a completed/failed job is kept before being pruned.
	jobTTL = 15 * time.Minute

	// pendingJobTTL keeps slow jobs visible for at least the full AI deadline.
	pendingJobTTL = AIOverallTimeout + time.Minute
)

// AIJob holds the state and result of one AI generation request.
type AIJob struct {
	ID        string
	Status    JobStatus
	Content   string
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AIJobStore is a thread-safe in-memory store for async AI jobs.
type AIJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*AIJob
}

var globalAIJobStore = &AIJobStore{jobs: make(map[string]*AIJob)}

// GetAIJobStore returns the singleton job store.
func GetAIJobStore() *AIJobStore { return globalAIJobStore }

// Create registers a new pending job and returns it.
func (s *AIJobStore) Create(id string) *AIJob {
	now := time.Now()
	job := &AIJob{ID: id, Status: JobPending, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	return cloneAIJob(job)
}

// Get looks up a job. Returns (nil, false) if not found or expired.
func (s *AIJobStore) Get(id string) (*AIJob, bool) {
	now := time.Now()

	s.mu.RLock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.RUnlock()
		return nil, false
	}

	expired := jobExpired(job, now)
	snapshot := cloneAIJob(job)
	s.mu.RUnlock()

	if expired {
		s.deleteIfExpired(id, now)
		return nil, false
	}

	return snapshot, true
}

// Complete marks a job as done with its generated content.
func (s *AIJobStore) Complete(id, content string) {
	now := time.Now()

	s.mu.Lock()
	if job, ok := s.jobs[id]; ok {
		job.Status = JobDone
		job.Content = content
		job.Error = ""
		job.UpdatedAt = now
	}
	s.mu.Unlock()
}

// Fail marks a job as failed with an error message.
func (s *AIJobStore) Fail(id, errMsg string) {
	now := time.Now()

	s.mu.Lock()
	if job, ok := s.jobs[id]; ok {
		job.Status = JobFailed
		job.Content = ""
		job.Error = errMsg
		job.UpdatedAt = now
	}
	s.mu.Unlock()
}

// Prune removes all jobs older than jobTTL. Call periodically from a goroutine.
func (s *AIJobStore) Prune() {
	now := time.Now()

	s.mu.Lock()
	for id, job := range s.jobs {
		if jobExpired(job, now) {
			delete(s.jobs, id)
		}
	}
	s.mu.Unlock()
}

func (s *AIJobStore) deleteIfExpired(id string, now time.Time) {
	s.mu.Lock()
	if job, ok := s.jobs[id]; ok && jobExpired(job, now) {
		delete(s.jobs, id)
	}
	s.mu.Unlock()
}

func jobExpired(job *AIJob, now time.Time) bool {
	if job == nil {
		return true
	}

	if job.Status == JobPending {
		return now.Sub(job.CreatedAt) > pendingJobTTL
	}

	return now.Sub(job.UpdatedAt) > jobTTL
}

func cloneAIJob(job *AIJob) *AIJob {
	if job == nil {
		return nil
	}

	copied := *job
	return &copied
}
