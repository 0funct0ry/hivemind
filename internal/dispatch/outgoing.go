// Package dispatch owns the fixed-size worker pool that delivers outgoing-webhook events
// (SPEC.md §4.11, M22) without ever running on a message-post request goroutine.
package dispatch

import (
	"context"
	"log/slog"

	"github.com/0funct0ry/hivemind/internal/store"
)

// queueCapacity bounds the number of pending dispatch jobs. A full queue drops the job rather
// than blocking the caller — an outgoing webhook is best-effort by design and must never
// threaten the availability of message posting itself (SPEC.md §4.11).
const queueCapacity = 256

// workerCount is the number of goroutines draining the dispatch queue.
const workerCount = 4

// Job is one outgoing-webhook delivery to perform.
type Job struct {
	Webhook store.OutgoingWebhook
	Event   store.OutgoingEvent
}

// Dispatcher is a fixed-size worker pool over a bounded job queue.
type Dispatcher struct {
	jobs chan Job
}

// NewDispatcher starts workerCount goroutines reading from a bounded queue and calling
// store.DispatchOutgoingWebhook for each job. There is no graceful shutdown — matching this
// repo's "no scheduler, no background worker framework" posture, the same as
// internal/realtime.Hub, which also has no explicit stop wired into cmd/serve.go.
func NewDispatcher(s *store.Store) *Dispatcher {
	d := &Dispatcher{jobs: make(chan Job, queueCapacity)}
	for i := 0; i < workerCount; i++ {
		go d.worker(s)
	}
	return d
}

func (d *Dispatcher) worker(s *store.Store) {
	for job := range d.jobs {
		if err := s.DispatchOutgoingWebhook(context.Background(), job.Webhook, job.Event); err != nil {
			slog.Warn("outgoing webhook delivery failed", "webhook_id", job.Webhook.ID, "error", err)
		}
	}
}

// Enqueue submits a job for delivery. It never blocks: if the queue is full, the job is
// dropped and the caller should log at warn (SPEC.md §4.11).
func (d *Dispatcher) Enqueue(job Job) bool {
	select {
	case d.jobs <- job:
		return true
	default:
		return false
	}
}
