package domain

import "time"

type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobDone       JobStatus = "done"
	JobFailed     JobStatus = "failed"
)

type Job struct {
	ID         string
	DocumentID string
	OwnerID    string
	Status     JobStatus
	Attempts   int
	Error      string
	BundleID   *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
