// ----- dead letter queue (Redis list) @ backend/internal/jobs/dlq.go -----
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

const dlqKey = "dlq:ingest"

// DLQEntry represents a failed job entry in the dead letter queue.
type DLQEntry struct {
	JobID      string                 `json:"job_id"`
	Task       string                 `json:"task"`
	Args       map[string]interface{} `json:"args"`
	Error      string                 `json:"error"`
	Timestamp  string                 `json:"timestamp"`
	RetryCount int                    `json:"retry_count"`
}

// PushToDLQ pushes a permanently failed job to the dead letter queue.
func PushToDLQ(jobID, taskName string, args map[string]interface{}, errMsg string, retryCount int) {
	rds := getRedis()
	if rds == nil {
		logger.Error("Cannot push to DLQ: redis not configured")
		return
	}

	entry := DLQEntry{
		JobID:      jobID,
		Task:       taskName,
		Args:       args,
		Error:      errMsg,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		RetryCount: retryCount,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		logger.Error("Failed to marshal DLQ entry: %s", err)
		return
	}

	ctx := context.Background()
	if err := rds.RPush(ctx, dlqKey, data).Err(); err != nil {
		logger.Error("Failed to push to DLQ: %s", err)
		return
	}

	logger.Warning("Pushed job %s to DLQ (retries: %d)", jobID, retryCount)
}

// PopDLQEntries pops and returns entries from the DLQ. If count is 0, pops all.
func PopDLQEntries(count int) []DLQEntry {
	rds := getRedis()
	if rds == nil {
		logger.Error("Cannot pop DLQ: redis not configured")
		return nil
	}

	ctx := context.Background()

	if count <= 0 {
		l, err := rds.LLen(ctx, dlqKey).Result()
		if err != nil {
			logger.Error("Failed to get DLQ length: %s", err)
			return nil
		}
		count = int(l)
	}

	var entries []DLQEntry
	for i := 0; i < count; i++ {
		raw, err := rds.LPop(ctx, dlqKey).Result()
		if err != nil {
			break
		}

		var entry DLQEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			logger.Error("Failed to unmarshal DLQ entry: %s", err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// GetDLQCount returns the number of entries in the DLQ.
func GetDLQCount() (int64, error) {
	rds := getRedis()
	if rds == nil {
		return 0, fmt.Errorf("redis not configured")
	}

	ctx := context.Background()
	return rds.LLen(ctx, dlqKey).Result()
}
