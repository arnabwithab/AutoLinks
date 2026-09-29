// ----- ingestion job manager (Redis-backed) @ backend/internal/jobs/manager.go -----
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	jobNamespace = "autolinks:job"
	jobTTL       = 86400 * 7 // 7 days
)

// ErrNotConfigured is returned when Redis has not been configured at all.
// It is distinguishable from a transient Redis failure (connection error).
var ErrNotConfigured = errors.New("redis not configured")

var (
	rdb     *redis.Client
	rdbOnce sync.Once
)

func getRedis() *redis.Client {
	rdbOnce.Do(func() {
		redisURL := config.RedisURL()
		if redisURL == "" {
			return
		}
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			logger.Error("Failed to parse Redis URL: %s", err)
			return
		}
		rdb = redis.NewClient(opts)
	})
	return rdb
}

// Job represents the state of an async ingest job.
type Job struct {
	JobID         string                 `json:"job_id"`
	Status        string                 `json:"status"`
	TaskName      string                 `json:"task_name"`
	Args          map[string]interface{} `json:"args"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
	ArticlesDone  int                    `json:"articles_done"`
	ArticlesTotal int                    `json:"articles_total"`
	Errors        []string               `json:"errors"`
}

// CreateJob creates a new job entry in Redis and returns its job_id.
func CreateJob(taskName string, args map[string]interface{}) (string, error) {
	rds := getRedis()
	if rds == nil {
		return "", ErrNotConfigured
	}

	jobID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	job := Job{
		JobID:         jobID,
		Status:        "queued",
		TaskName:      taskName,
		Args:          args,
		CreatedAt:     now,
		UpdatedAt:     now,
		ArticlesDone:  0,
		ArticlesTotal: 0,
		Errors:        []string{},
	}

	data, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("failed to marshal job: %w", err)
	}

	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", jobNamespace, jobID)
	if err := rds.Set(ctx, key, data, time.Duration(jobTTL)*time.Second).Err(); err != nil {
		return "", fmt.Errorf("failed to save job: %w", err)
	}

	logger.Info("Created job %s (%s)", jobID, taskName)
	return jobID, nil
}

// GetJob retrieves a job by ID from Redis.
func GetJob(jobID string) (*Job, error) {
	rds := getRedis()
	if rds == nil {
		return nil, ErrNotConfigured
	}

	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", jobNamespace, jobID)
	raw, err := rds.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	var job Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	return &job, nil
}

// UpdateJob updates fields on a job. It is a read-modify-write, not atomic —
// safe only while a single worker owns a given job. Use WATCH/Lua if a second
// writer is ever introduced.
func UpdateJob(jobID string, updates map[string]interface{}) error {
	rds := getRedis()
	if rds == nil {
		return ErrNotConfigured
	}

	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", jobNamespace, jobID)
	raw, err := rds.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get job for update: %w", err)
	}

	var job map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return fmt.Errorf("failed to unmarshal job: %w", err)
	}

	for k, v := range updates {
		job[k] = v
	}
	job["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	if err := rds.Set(ctx, key, data, time.Duration(jobTTL)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to save job: %w", err)
	}

	return nil
}

// PendingJobs returns jobs left in a non-terminal state (queued, processing, retrying).
func PendingJobs() ([]*Job, error) {
	rds := getRedis()
	if rds == nil {
		return nil, ErrNotConfigured
	}

	ctx := context.Background()
	var cursor uint64
	var pending []*Job

	for {
		keys, next, err := rds.Scan(ctx, cursor, jobNamespace+":*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan jobs: %w", err)
		}
		for _, key := range keys {
			raw, getErr := rds.Get(ctx, key).Result()
			if getErr != nil {
				continue
			}
			var job Job
			if json.Unmarshal([]byte(raw), &job) != nil {
				continue
			}
			switch job.Status {
			case "queued", "processing", "retrying":
				jobCopy := job
				pending = append(pending, &jobCopy)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}

	return pending, nil
}

// Health pings Redis and returns an error if it is unreachable.
func Health(ctx context.Context) error {
	rds := getRedis()
	if rds == nil {
		return ErrNotConfigured
	}
	return rds.Ping(ctx).Err()
}

// AddJobError appends an error to a job's error list.
func AddJobError(jobID string, errorMsg string) error {
	rds := getRedis()
	if rds == nil {
		return ErrNotConfigured
	}

	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", jobNamespace, jobID)
	raw, err := rds.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	var job map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return fmt.Errorf("failed to unmarshal job: %w", err)
	}

	errors, _ := job["errors"].([]interface{})
	errors = append(errors, errorMsg)
	job["errors"] = errors
	job["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	if err := rds.Set(ctx, key, data, time.Duration(jobTTL)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to save job: %w", err)
	}

	return nil
}
