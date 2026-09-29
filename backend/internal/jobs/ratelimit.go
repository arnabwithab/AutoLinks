// ----- shared rate-limit counter @ backend/internal/jobs/ratelimit.go -----
package jobs

import (
	"context"
	"fmt"
	"time"
)

// RateLimitCount atomically increments a fixed-window counter and returns the
// current count. TTL is set on first hit; INCR itself is atomic so concurrent
// API tasks share one budget (§3) with no locking.
// ponytail: plain INCR+EXPIRE, no Lua. A lost EXPIRE leaks one key — acceptable.
func RateLimitCount(ctx context.Context, key string, window time.Duration) (int64, error) {
	rds := getRedis()
	if rds == nil {
		return 0, ErrNotConfigured
	}
	n, err := rds.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("rate limit incr failed: %w", err)
	}
	if n == 1 {
		if err := rds.Expire(ctx, key, window).Err(); err != nil {
			return 0, fmt.Errorf("rate limit expire failed: %w", err)
		}
	}
	return n, nil
}
