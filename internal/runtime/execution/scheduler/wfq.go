package scheduler

import (
	"container/heap"
	"context"
	"errors"
	"sync"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

// Job represents a schedulable unit of work.
type Job struct {
	ID         string
	Namespace  string
	Priority   int
	EnqueuedAt time.Time
	Execute    func(context.Context) error

	// Internal scheduling fields
	virtualFinishTime float64
}

// WFQScheduler implements weighted fair queueing for namespace-based scheduling.
type WFQScheduler struct {
	mu            sync.Mutex
	queues        map[string]*namespaceQueue
	globalQueue   *priorityQueue
	weights       map[string]int
	defaultWeight int
	virtualTime   float64
	running       bool
	stopCh        chan struct{}
	workerCount   int
	workCh        chan *Job
}

// namespaceQueue tracks per-namespace state.
type namespaceQueue struct {
	namespace       string
	weight          int
	virtualTime     float64
	jobCount        int
	lastScheduledAt time.Time
}

// Config defines WFQ scheduler configuration.
type Config struct {
	// DefaultWeight is the weight assigned to namespaces without explicit config.
	DefaultWeight int
	// WorkerCount is the number of concurrent workers executing jobs.
	WorkerCount int
}

// DefaultConfig returns sensible defaults for WFQ scheduler.
func DefaultConfig() Config {
	return Config{
		DefaultWeight: 1,
		WorkerCount:   4,
	}
}

// NewWFQScheduler creates a new weighted fair queueing scheduler.
func NewWFQScheduler(config Config) *WFQScheduler {
	if config.DefaultWeight <= 0 {
		config.DefaultWeight = 1
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = 4
	}

	return &WFQScheduler{
		queues:        make(map[string]*namespaceQueue),
		globalQueue:   &priorityQueue{},
		weights:       make(map[string]int),
		defaultWeight: config.DefaultWeight,
		workerCount:   config.WorkerCount,
		workCh:        make(chan *Job, config.WorkerCount*2),
		stopCh:        make(chan struct{}),
	}
}

// SetWeight configures the scheduling weight for a namespace.
// Higher weight means more capacity share (weight 2 gets 2x of weight 1).
func (s *WFQScheduler) SetWeight(namespace string, weight int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if weight <= 0 {
		weight = s.defaultWeight
	}

	s.weights[namespace] = weight

	// Update existing queue if present
	if q, exists := s.queues[namespace]; exists {
		q.weight = weight
	}
}

// GetWeight returns the configured weight for a namespace.
func (s *WFQScheduler) GetWeight(namespace string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if weight, exists := s.weights[namespace]; exists {
		return weight
	}
	return s.defaultWeight
}

// Enqueue adds a job to the scheduler queue.
func (s *WFQScheduler) Enqueue(job *Job) error {
	if job == nil {
		return errors.New("job cannot be nil")
	}
	if job.Execute == nil {
		return errors.New("job execute function cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get or create namespace queue
	nsQueue := s.getOrCreateQueue(job.Namespace)

	// Calculate virtual finish time for this job
	// Virtual finish time = max(current virtual time, queue virtual time) + (1 / weight)
	startVTime := s.virtualTime
	if nsQueue.virtualTime > startVTime {
		startVTime = nsQueue.virtualTime
	}

	job.virtualFinishTime = startVTime + (1.0 / float64(nsQueue.weight))

	// Update namespace queue state
	nsQueue.virtualTime = job.virtualFinishTime
	nsQueue.jobCount++

	// Add to global priority queue (ordered by virtual finish time)
	heap.Push(s.globalQueue, job)

	// Job will be dispatched by schedulerLoop based on virtual finish time

	return nil
}

// Start begins the scheduler workers.
//
// Index:
// - Purpose: Start worker pool and WFQ dispatch loop
// - Flow: spawn workers → start scheduler loop → return
// - SideEffects: starts goroutines
// - FailureModes: none (best-effort start)
// - Related: WFQScheduler.schedulerLoop, WFQScheduler.worker
// - Keywords: wfq_start, workers, scheduler_loop
func (s *WFQScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	// Start worker goroutines
	for i := 0; i < s.workerCount; i++ {
		go s.worker(ctx, i)
	}

	// Start scheduler loop
	go s.schedulerLoop(ctx)
}

// Stop gracefully stops the scheduler.
func (s *WFQScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	// Do not close workCh here to avoid panic in dispatchNext if it tries to send
	// Workers will exit on stopCh close
}

// schedulerLoop continuously dispatches jobs to workers.
//
// Index:
// - Purpose: Dispatch jobs based on virtual finish time
// - Flow: tick → select next job → send to worker queue
// - SideEffects: sends jobs to work channel
// - FailureModes: none (drops if scheduler stopped)
// - Related: WFQScheduler.dispatchNext
// - Keywords: wfq_dispatch, virtual_time, priority_queue
func (s *WFQScheduler) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.dispatchNext()
		}
	}
}

// dispatchNext sends the next job with smallest virtual finish time to workers.
//
// Index:
// - Purpose: Select next job and enqueue for execution
// - Flow: pop heap → update virtual times → send to work channel
// - SideEffects: updates queue state; sends job to workers
// - FailureModes: none
// - Related: WFQScheduler.schedulerLoop
// - Keywords: dispatch_next, virtual_finish, namespace_queue
func (s *WFQScheduler) dispatchNext() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.globalQueue.Len() == 0 {
		return
	}

	// Pop job with smallest virtual finish time
	item := heap.Pop(s.globalQueue)
	job, ok := item.(*Job)
	if !ok {
		// Should never happen, but handle gracefully
		return
	}

	// Update global virtual time to job's finish time
	s.virtualTime = job.virtualFinishTime

	// Send to worker channel (non-blocking)
	select {
	case s.workCh <- job:
	default:
		// Workers busy, re-enqueue
		heap.Push(s.globalQueue, job)
	}
}

// worker executes jobs from the work channel.
func (s *WFQScheduler) worker(ctx context.Context, _ int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case job, ok := <-s.workCh:
			if !ok {
				return
			}

			// Execute the job
			errs.Ignore(job.Execute(ctx), "scheduler: execute job")

			// Update namespace stats
			s.mu.Lock()
			if nsQueue, exists := s.queues[job.Namespace]; exists {
				nsQueue.jobCount--
				nsQueue.lastScheduledAt = time.Now()
			}
			s.mu.Unlock()
		}
	}
}

// getOrCreateQueue returns existing queue or creates new one (must hold lock).
func (s *WFQScheduler) getOrCreateQueue(namespace string) *namespaceQueue {
	if q, exists := s.queues[namespace]; exists {
		return q
	}

	weight := s.defaultWeight
	if w, exists := s.weights[namespace]; exists {
		weight = w
	}

	q := &namespaceQueue{
		namespace:   namespace,
		weight:      weight,
		virtualTime: s.virtualTime,
	}
	s.queues[namespace] = q
	return q
}

// Stats returns current scheduler statistics.
func (s *WFQScheduler) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	nsStats := make([]NamespaceStats, 0, len(s.queues))
	for _, q := range s.queues {
		nsStats = append(nsStats, NamespaceStats{
			Namespace:       q.namespace,
			Weight:          q.weight,
			VirtualTime:     q.virtualTime,
			PendingJobs:     q.jobCount,
			LastScheduledAt: q.lastScheduledAt,
		})
	}

	return Stats{
		VirtualTime:     s.virtualTime,
		QueuedJobs:      s.globalQueue.Len(),
		NamespaceQueues: nsStats,
	}
}

// Stats holds scheduler statistics.
type Stats struct {
	VirtualTime     float64          `json:"virtual_time"`
	QueuedJobs      int              `json:"queued_jobs"`
	NamespaceQueues []NamespaceStats `json:"namespace_queues"`
}

// NamespaceStats holds per-namespace statistics.
type NamespaceStats struct {
	Namespace       string    `json:"namespace"`
	Weight          int       `json:"weight"`
	VirtualTime     float64   `json:"virtual_time"`
	PendingJobs     int       `json:"pending_jobs"`
	LastScheduledAt time.Time `json:"last_scheduled_at,omitempty"`
}

// priorityQueue implements heap.Interface for virtual finish time ordering.
type priorityQueue []*Job

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].virtualFinishTime < pq[j].virtualFinishTime
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x any) {
	job, ok := x.(*Job)
	if !ok {
		// Should never happen with correct usage, but handle gracefully
		return
	}
	*pq = append(*pq, job)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
