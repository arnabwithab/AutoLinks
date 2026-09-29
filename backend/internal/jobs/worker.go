// ----- goroutine worker pool with retry and DLQ @ backend/internal/jobs/worker.go -----
package jobs

import (
	"fmt"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/ingest"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
)

const (
	maxWorkers         = 4
	maxConcurrent      = 5
	minConcurrent      = 1
	maxConcurrentLimit = 20
	maxRetries         = 3
	baseDelay          = 30 * time.Second
	jobsChanBuffer     = 100
	maxRecordedErrors  = 10
)

// WorkerPool manages a pool of goroutines that process ingest jobs.
type WorkerPool struct {
	jobs chan *Job
	wg   sync.WaitGroup
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool() *WorkerPool {
	wp := &WorkerPool{
		jobs: make(chan *Job, jobsChanBuffer),
	}

	for i := 0; i < maxWorkers; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(i)
	}

	logger.Info("Worker pool started with %d goroutines", maxWorkers)
	return wp
}

// Enqueue submits a job without blocking. It returns false if the queue is full.
func (wp *WorkerPool) Enqueue(job *Job) bool {
	select {
	case wp.jobs <- job:
		return true
	default:
		logger.Warning("Job queue full, refusing job %s", job.JobID)
		return false
	}
}

// Stop closes the queue and waits for in-flight jobs to finish.
func (wp *WorkerPool) Stop() {
	close(wp.jobs)
	wp.wg.Wait()
}

// ReconcileJobs re-enqueues jobs left in a non-terminal state by a previous
// process (deploy, crash, or restart). Single-instance assumption: it does not
// coordinate with another running replica.
func ReconcileJobs(pool *WorkerPool) int {
	pending, err := PendingJobs()
	if err != nil {
		logger.Error("Failed to list pending jobs: %s", err)
		return 0
	}

	enqueued := 0
	for _, job := range pending {
		if uErr := UpdateJob(job.JobID, map[string]interface{}{"status": "queued"}); uErr != nil {
			logger.Error("Failed to reset pending job %s: %s", job.JobID, uErr)
			continue
		}
		job.Status = "queued"
		if pool.Enqueue(job) {
			enqueued++
		}
	}
	return enqueued
}

func (wp *WorkerPool) workerLoop(workerID int) {
	defer wp.wg.Done()
	for job := range wp.jobs {
		logger.Info("Worker %d processing job %s", workerID, job.JobID)
		wp.processJobSafely(job)
	}
}

// processJobSafely isolates a job so a panic cannot take down the process.
func (wp *WorkerPool) processJobSafely(job *Job) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Worker panic on job %s: %v", job.JobID, r)
			if uErr := UpdateJob(job.JobID, map[string]interface{}{"status": "failed"}); uErr != nil {
				logger.Error("Failed to mark panicked job %s failed: %s", job.JobID, uErr)
			}
			if aErr := AddJobError(job.JobID, fmt.Sprintf("worker panic: %v", r)); aErr != nil {
				logger.Error("Failed to record panic for job %s: %s", job.JobID, aErr)
			}
		}
	}()
	wp.processJobWithRetry(job)
}

func (wp *WorkerPool) processJobWithRetry(job *Job) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := wp.processJob(job)
		if err == nil {
			if uErr := UpdateJob(job.JobID, map[string]interface{}{
				"status":        "done",
				"articles_done": job.ArticlesDone,
			}); uErr != nil {
				logger.Error("Failed to update job %s status to done: %s", job.JobID, uErr)
			}
			return
		}

		lastErr = err
		logger.Error("Job %s failed (attempt %d/%d): %s", job.JobID, attempt+1, maxRetries, err)

		if attempt < maxRetries {
			delay := baseDelay * time.Duration(1<<uint(attempt))
			logger.Info("Retrying job %s in %s", job.JobID, delay)
			if uErr := UpdateJob(job.JobID, map[string]interface{}{"status": "retrying"}); uErr != nil {
				logger.Error("Failed to update job %s status: %s", job.JobID, uErr)
			}
			time.Sleep(delay)
		}
	}

	PushToDLQ(job.JobID, job.TaskName, job.Args, lastErr.Error(), maxRetries)

	if uErr := UpdateJob(job.JobID, map[string]interface{}{"status": "failed"}); uErr != nil {
		logger.Error("Failed to update job %s status to failed: %s", job.JobID, uErr)
	}
	if aErr := AddJobError(job.JobID, lastErr.Error()); aErr != nil {
		logger.Error("Failed to add error to job %s: %s", job.JobID, aErr)
	}
}

func (wp *WorkerPool) processJob(job *Job) error {
	if uErr := UpdateJob(job.JobID, map[string]interface{}{"status": "processing"}); uErr != nil {
		logger.Error("Failed to update job %s status to processing: %s", job.JobID, uErr)
	}

	sitemapURL, ok := job.Args["sitemap_url"].(string)
	if !ok {
		return fmt.Errorf("sitemap_url not found in job args")
	}

	concurrency := resolveConcurrency(job.Args)

	urls := ingest.ParseSitemap(sitemapURL)
	if len(urls) == 0 {
		return fmt.Errorf("no URLs found in sitemap")
	}

	if uErr := UpdateJob(job.JobID, map[string]interface{}{
		"articles_total": len(urls),
		"articles_done":  0,
	}); uErr != nil {
		logger.Error("Failed to update job %s totals: %s", job.JobID, uErr)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	outboundMap := make(map[string][]string)
	var failures []string
	done := 0

	urlCh := make(chan string)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range urlCh {
				links, err := processURL(u)
				mu.Lock()
				if err != nil {
					failures = append(failures, u+": "+err.Error())
				} else {
					done++
					outboundMap[u] = links
				}
				mu.Unlock()
			}
		}()
	}

	for _, u := range urls {
		urlCh <- u
	}
	close(urlCh)
	wg.Wait()

	if uErr := UpdateJob(job.JobID, map[string]interface{}{"articles_done": done}); uErr != nil {
		logger.Error("Failed to update job %s done count: %s", job.JobID, uErr)
	}

	job.ArticlesDone = done

	for i, f := range failures {
		if i >= maxRecordedErrors {
			_ = AddJobError(job.JobID, fmt.Sprintf("%d more page failures not shown", len(failures)-maxRecordedErrors))
			break
		}
		if aErr := AddJobError(job.JobID, f); aErr != nil {
			logger.Error("Failed to record page failure for job %s: %s", job.JobID, aErr)
		}
	}

	if len(outboundMap) > 0 {
		pages := make(ingest.PageMap)
		for url, lnks := range outboundMap {
			pages[url] = &ingest.PageData{OutboundLinks: lnks}
		}
		rerank.MergeLinkGraph(ingest.BuildLinkGraph(pages))
	}

	if done == 0 {
		return fmt.Errorf("all %d pages failed", len(urls))
	}

	return nil
}

func processURL(u string) (links []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return ingest.StreamFetchEmbedUpsert(u)
}

func resolveConcurrency(args map[string]interface{}) int {
	concurrency := maxConcurrent
	if v, ok := args["max_concurrent"]; ok {
		switch n := v.(type) {
		case float64:
			concurrency = int(n)
		case int:
			concurrency = n
		}
	}
	if concurrency < minConcurrent {
		concurrency = minConcurrent
	}
	if concurrency > maxConcurrentLimit {
		concurrency = maxConcurrentLimit
	}
	return concurrency
}
