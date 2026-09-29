// ----- Redis Streams job queue with lease claim @ backend/internal/jobs/queue.go -----
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrNotQueued is returned when a claim loses the race (job already taken).
var ErrNotQueued = errors.New("job not in queued state")

// EnsureStreamGroup creates the consumer group idempotently.
func EnsureStreamGroup(ctx context.Context) error {
	rds := getRedis()
	if rds == nil {
		return ErrNotConfigured
	}
	err := rds.XGroupCreateMkStream(ctx, streamKey, streamGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create stream group: %w", err)
	}
	return nil
}

// EnqueueStream publishes a job to the shared stream (cross-process §1 slow path).
func EnqueueStream(ctx context.Context, jobID, fingerprint, traceID string) (string, error) {
	rds := getRedis()
	if rds == nil {
		return "", ErrNotConfigured
	}
	msgID, err := rds.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"job_id":      jobID,
			"fingerprint": fingerprint,
			"trace_id":    traceID,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("failed to enqueue job: %w", err)
	}
	return msgID, nil
}

// StreamMessage is a claimed stream entry.
type StreamMessage struct {
	MsgID       string
	JobID       string
	Fingerprint string
	TraceID     string
}

// ReadClaim blocks for one job on the stream for consumer, then CAS-claims it.
// A losing racer gets (nil, ErrNotQueued): polite loser, message acked.
func ReadClaim(ctx context.Context, consumer string, block time.Duration) (*StreamMessage, error) {
	rds := getRedis()
	if rds == nil {
		return nil, ErrNotConfigured
	}
	streams, err := rds.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    streamGroup,
		Consumer: consumer,
		Streams:  []string{streamKey, ">"},
		Count:    1,
		Block:    block,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("failed to read stream: %w", err)
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, nil
	}
	m := streams[0].Messages[0]
	sm := &StreamMessage{
		MsgID:       m.ID,
		JobID:       toString(m.Values["job_id"]),
		Fingerprint: toString(m.Values["fingerprint"]),
		TraceID:     toString(m.Values["trace_id"]),
	}
	if sm.JobID == "" {
		_ = rds.XAck(ctx, streamKey, streamGroup, m.ID).Err()
		return nil, fmt.Errorf("stream message %s missing job_id", m.ID)
	}

	lease := uuid.New().String()
	claimed, err := compareAndClaim(ctx, rds, sm.JobID, lease)
	if err != nil {
		_ = rds.XAck(ctx, streamKey, streamGroup, m.ID).Err()
		return nil, err
	}
	if !claimed {
		// Lost the race: ack so it is not redelivered to us; winner owns it.
		_ = rds.XAck(ctx, streamKey, streamGroup, m.ID).Err()
		return nil, ErrNotQueued
	}
	return sm, nil
}

// AckStream acknowledges a finished message.
func AckStream(ctx context.Context, msgID string) error {
	rds := getRedis()
	if rds == nil {
		return ErrNotConfigured
	}
	return rds.XAck(ctx, streamKey, streamGroup, msgID).Err()
}

// QueueDepth returns pending stream entries (for backlog-per-task §6).
func QueueDepth(ctx context.Context) (int64, error) {
	rds := getRedis()
	if rds == nil {
		return 0, ErrNotConfigured
	}
	return rds.XLen(ctx, streamKey).Result()
}

// compareAndClaim sets status=processing + lease only if status is still queued.
// WATCH gives optimistic locking: concurrent racers produce one winner and
// TxFailedErr losers (mapped to claimed=false, no error).
func compareAndClaim(ctx context.Context, rds *redis.Client, jobID, lease string) (bool, error) {
	key := fmt.Sprintf("%s:%s", jobNamespace, jobID)
	now := time.Now().UTC().Format(time.RFC3339)
	expires := time.Now().UTC().Add(time.Duration(leaseTTLSeconds) * time.Second).Format(time.RFC3339)

	for attempt := 0; attempt < 3; attempt++ {
		err := rds.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Result()
			if err != nil {
				return err
			}
			var job Job
			if err := json.Unmarshal([]byte(raw), &job); err != nil {
				return err
			}
			if job.Status != "queued" {
				return ErrNotQueued
			}
			job.Status = "processing"
			job.LeaseToken = lease
			job.LeaseExpiresAt = expires
			job.UpdatedAt = now
			data, err := json.Marshal(job)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				return pipe.Set(ctx, key, data, time.Duration(jobTTL)*time.Second).Err()
			})
			return err
		}, key)

		if err == nil {
			return true, nil
		}
		if errors.Is(err, ErrNotQueued) {
			return false, nil
		}
		if errors.Is(err, redis.TxFailedErr) {
			continue // lost race, retry reads current state
		}
		return false, fmt.Errorf("claim failed: %w", err)
	}
	return false, nil
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ReclaimOrphans re-queues stream messages idle past the lease TTL (§7 worker dies).
// It returns the number of messages reclaimed for redelivery.
func ReclaimOrphans(ctx context.Context, consumer string) (int, error) {
	rds := getRedis()
	if rds == nil {
		return 0, ErrNotConfigured
	}
	var start string = "0-0"
	reclaimed := 0
	for {
		res, next, err := rds.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   streamKey,
			Group:    streamGroup,
			Consumer: consumer,
			MinIdle:  time.Duration(leaseTTLSeconds) * time.Second,
			Start:    start,
			Count:    50,
		}).Result()
		if err != nil {
			return reclaimed, fmt.Errorf("autoclaim failed: %w", err)
		}
		reclaimed += len(res)
		if next == "0-0" || len(res) == 0 {
			break
		}
		start = next
		logger.Info("Reclaimed %d orphan stream messages", reclaimed)
		return reclaimed, nil
	}
	return reclaimed, nil
}
