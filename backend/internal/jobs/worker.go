// ----- goroutine worker pool with retry and DLQ @ backend/internal/jobs/worker.go -----
package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/ingest"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/rerank"
	"golang.org/x/sync/semaphore"
)

const (
	maxWorkers     = 4
	maxConcurrent  = 5
	maxRetries     = 3
	baseDelay      = 30 * time.Second
	jobsChanBuffer = 100
)

// WorkerPool manages a pool of goroutines that process ingest jobs.
type WorkerPool struct {
	jobs chan *Job
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool() *WorkerPool {
	wp := &WorkerPool{
		jobs: make(chan *Job, jobsChanBuffer),
	}

	for i := 0; i < maxWorkers; i++ {
		go wp.workerLoop(i)
	}

	logger.Info("Worker pool started with %d goroutines", maxWorkers)
	return wp
}

// Enqueue submits a job to the worker pool for processing.
func (wp *WorkerPool) Enqueue(job *Job) {
	wp.jobs <- job
}

func (wp *WorkerPool) workerLoop(workerID int) {
	for job := range wp.jobs {
		logger.Info("Worker %d processing job %s", workerID, job.JobID)
		wp.processJobWithRetry(job)
	}
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

	outboundMap := make(map[string][]string)
	done := 0

	sem := semaphore.NewWeighted(maxConcurrent)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var jobErr error
	ctx := context.Background()

	for _, rawURL := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			if err := sem.Acquire(ctx, 1); err != nil {
				mu.Lock()
				if jobErr == nil {
					jobErr = fmt.Errorf("semaphore acquire: %w", err)
				}
				mu.Unlock()
				return
			}
			defer sem.Release(1)

			links := ingest.StreamFetchEmbedUpsert(url)

			mu.Lock()
			if links != nil {
				done++
				outboundMap[url] = links
			}
			mu.Unlock()
		}(rawURL)
	}

	wg.Wait()

	if jobErr != nil {
		return jobErr
	}

	_ = UpdateJob(job.JobID, map[string]interface{}{"articles_done": done})

	job.ArticlesDone = done

	if len(outboundMap) > 0 {
		pages := make(ingest.PageMap)
		for url, lnks := range outboundMap {
			pages[url] = &ingest.PageData{OutboundLinks: lnks}
		}
		graph := ingest.BuildLinkGraph(pages)
		rerank.InitLinkGraph(graph)
	}

	return nil
}
