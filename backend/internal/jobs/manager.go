// ----- ingestion job manager (Redis-backed) @ backend/internal/jobs/manager.go -----
package jobs

import (
	"context"
	"encoding/json"
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

var (
	rdb     *redis.Client
	rdbOnce sync.Once
	rdbErr  error
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
			rdbErr = err
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
		return "", fmt.Errorf("redis not configured")
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
		return nil, fmt.Errorf("redis not configured")
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

// UpdateJob atomically updates fields on a job.
func UpdateJob(jobID string, updates map[string]interface{}) error {
	rds := getRedis()
	if rds == nil {
		return fmt.Errorf("redis not configured")
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

// AddJobError appends an error to a job's error list.
func AddJobError(jobID string, errorMsg string) error {
	rds := getRedis()
	if rds == nil {
		return fmt.Errorf("redis not configured")
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
