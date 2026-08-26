// Package jobs is an in-process replacement for the upstream BullMQ/Redis
// pipeline. Queue names and job names match Immich's enum values so
// /api/jobs reports the same structure; handlers are registered per job
// name and processed by a small worker pool per queue.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// QueueName values — the 19 queues of QueueName in server/src/enum.ts.
const (
	QueueThumbnailGeneration    = "thumbnailGeneration"
	QueueMetadataExtraction     = "metadataExtraction"
	QueueVideoConversion        = "videoConversion"
	QueueFaceDetection          = "faceDetection"
	QueueFacialRecognition      = "facialRecognition"
	QueueSmartSearch            = "smartSearch"
	QueueDuplicateDetection     = "duplicateDetection"
	QueueBackgroundTask         = "backgroundTask"
	QueueStorageTemplateMigration = "storageTemplateMigration"
	QueueMigration              = "migration"
	QueueSearch                 = "search"
	QueueSidecar                = "sidecar"
	QueueLibrary                = "library"
	QueueNotifications          = "notifications"
	QueueBackupDatabase         = "backupDatabase"
	QueueOCR                    = "ocr"
	QueueWorkflow               = "workflow"
	QueueIntegrityCheck         = "integrityCheck"
	QueueEditor                 = "editor"
)

// AllQueues in the stable order the legacy /api/jobs response expects.
var AllQueues = []string{
	QueueBackgroundTask,
	QueueBackupDatabase,
	QueueDuplicateDetection,
	QueueEditor,
	QueueFaceDetection,
	QueueFacialRecognition,
	QueueIntegrityCheck,
	QueueLibrary,
	QueueMetadataExtraction,
	QueueMigration,
	QueueNotifications,
	QueueOCR,
	QueueSearch,
	QueueSidecar,
	QueueSmartSearch,
	QueueStorageTemplateMigration,
	QueueThumbnailGeneration,
	QueueVideoConversion,
	QueueWorkflow,
}

// JobName values used by this port (a subset of the ~60 upstream names).
const (
	JobAssetExtractMetadata    = "AssetExtractMetadata"
	JobAssetGenerateThumbnails = "AssetGenerateThumbnails"
	JobAssetEncodeVideo        = "AssetEncodeVideo"
	JobAssetDetectFaces        = "AssetDetectFaces"
	JobFacialRecognitionRun    = "FacialRecognition"
	JobSmartSearchRun          = "SmartSearch"
	JobDuplicateDetectionRun   = "DuplicateDetection"
	JobOcrRun                  = "Ocr"
	JobAssetDelete             = "AssetDelete"
	JobUserDelete              = "UserDelete"
	JobSessionCleanup          = "SessionCleanup"
)

// JobCounts is the QueueStatisticsDto shape served by /api/jobs.
type JobCounts struct {
	Active    int64 `json:"active"`
	Completed int64 `json:"completed"`
	Delayed   int64 `json:"delayed"`
	Failed    int64 `json:"failed"`
	Paused    int64 `json:"paused"`
	Waiting   int64 `json:"waiting"`
}

// QueueStatus is the QueueStatusLegacyDto shape.
type QueueStatus struct {
	IsActive  bool `json:"isActive"`
	IsPaused  bool `json:"isPaused"`
}

type job struct {
	name string
	data any
}

type queue struct {
	name       string
	concurrent int

	mu        sync.Mutex
	waiting   []job
	stats     JobCounts
	paused    bool
	wake      chan struct{}
}

func (q *queue) counts() (JobCounts, QueueStatus) {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := q.stats
	c.Waiting = int64(len(q.waiting))
	c.Paused = 0
	return c, QueueStatus{IsActive: c.Active > 0, IsPaused: q.paused}
}

type Handler func(ctx context.Context, name string, data any) error

// System routes jobs to handlers and runs one goroutine per queue.
type System struct {
	logger   *slog.Logger
	mu       sync.Mutex
	queues   map[string]*queue
	handlers map[string]Handler
	routes   map[string]string // job name -> queue name
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewSystem(logger *slog.Logger) *System {
	if logger == nil {
		logger = slog.Default()
	}
	s := &System{
		logger:   logger,
		queues:   map[string]*queue{},
		handlers: map[string]Handler{},
		routes:   map[string]string{},
	}
	for _, name := range AllQueues {
		s.queues[name] = &queue{name: name, concurrent: 1, wake: make(chan struct{}, 1)}
	}
	return s
}

// Register binds a job name to a queue and its handler. Mirroring the
// upstream startup check, Queue fails when a job has no handler only at
// run time; missing handlers log a warning instead of panicking.
func (s *System) Register(jobName, queueName string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[jobName] = h
	s.routes[jobName] = queueName
}

// Queue enqueues a job for asynchronous processing.
func (s *System) Queue(jobName string, data any) error {
	s.mu.Lock()
	route, ok := s.routes[jobName]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no handler registered for job %s", jobName)
	}
	q := s.queues[route]
	s.mu.Unlock()

	q.mu.Lock()
	q.waiting = append(q.waiting, job{name: jobName, data: data})
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Start launches workers for every queue.
func (s *System) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	for _, q := range s.orderedQueues() {
		s.wg.Add(1)
		go s.run(ctx, q)
	}
}

func (s *System) orderedQueues() []*queue {
	out := make([]*queue, 0, len(AllQueues))
	for _, name := range AllQueues {
		out = append(out, s.queues[name])
	}
	return out
}

// Stop cancels workers and waits for them to finish.
func (s *System) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *System) run(ctx context.Context, q *queue) {
	defer s.wg.Done()
	for {
		q.mu.Lock()
		paused := q.paused
		var next *job
		if !paused && len(q.waiting) > 0 {
			j := q.waiting[0]
			q.waiting = q.waiting[1:]
			q.stats.Active++
			next = &j
		}
		q.mu.Unlock()

		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		s.dispatch(ctx, q, next)
	}
}

func (s *System) dispatch(ctx context.Context, q *queue, j *job) {
	start := time.Now()
	s.mu.Lock()
	h := s.handlers[j.name]
	s.mu.Unlock()

	var err error
	if h == nil {
		err = fmt.Errorf("no handler for job %s", j.name)
	} else {
		err = h(ctx, j.name, j.data)
	}

	q.mu.Lock()
	q.stats.Active--
	if err != nil {
		q.stats.Failed++
	} else {
		q.stats.Completed++
	}
	q.mu.Unlock()

	if err != nil {
		s.logger.Warn("job failed", "job", j.name, "queue", q.name, "err", err, "took", time.Since(start))
	} else {
		s.logger.Debug("job done", "job", j.name, "queue", q.name, "took", time.Since(start))
	}
}

// Counts returns live statistics for /api/jobs.
func (s *System) Counts() map[string]struct {
	Counts JobCounts
	Status QueueStatus
} {
	out := map[string]struct {
		Counts JobCounts
		Status QueueStatus
	}{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, q := range s.queues {
		c, st := q.counts()
		out[name] = struct {
			Counts JobCounts
			Status QueueStatus
		}{c, st}
	}
	return out
}

// SetPaused toggles a queue (PUT /api/jobs/:name with pause/resume).
func (s *System) SetPaused(queueName string, paused bool) error {
	s.mu.Lock()
	q, ok := s.queues[queueName]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown queue %s", queueName)
	}
	q.mu.Lock()
	q.paused = paused
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Empty clears a queue's pending jobs (PUT /api/jobs/:name empty).
func (s *System) Empty(queueName string) error {
	s.mu.Lock()
	q, ok := s.queues[queueName]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown queue %s", queueName)
	}
	q.mu.Lock()
	q.waiting = nil
	q.mu.Unlock()
	return nil
}
