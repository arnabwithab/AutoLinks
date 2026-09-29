// ----- sitemap fingerprint for submit dedup @ backend/internal/jobs/fingerprint.go -----
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/ingest"
)

// FingerprintURL hashes the canonical sitemap URL so resubmits return the
// existing job instead of starting a duplicate crawl (§2 Submit).
func FingerprintURL(rawURL string) string {
	canonical := ingest.NormalizeURL(rawURL)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// CreateJobDeduped creates a job unless fingerprint already maps to one, in
// which case the existing job ID is returned. Empty fingerprint skips dedup.
func CreateJobDeduped(taskName string, args map[string]interface{}, fingerprint, traceID string) (jobID string, created bool, err error) {
	rds := getRedis()
	if rds == nil {
		return "", false, ErrNotConfigured
	}
	ctx := context.Background()

	if fingerprint != "" {
		fkey := fingerprintNamespace + ":" + fingerprint
		if existing, gErr := rds.Get(ctx, fkey).Result(); gErr == nil && existing != "" {
			if _, jErr := GetJob(existing); jErr == nil {
				return existing, false, nil
			}
		}
	}

	id, cErr := CreateJob(taskName, args)
	if cErr != nil {
		return "", false, cErr
	}

	if fingerprint != "" || traceID != "" {
		updates := map[string]interface{}{}
		if fingerprint != "" {
			updates["fingerprint"] = fingerprint
		}
		if traceID != "" {
			updates["trace_id"] = traceID
		}
		_ = UpdateJob(id, updates)
		if fingerprint != "" {
			_ = rds.Set(ctx, fingerprintNamespace+":"+fingerprint, id, time.Duration(jobTTL)*time.Second).Err()
		}
	}

	return id, true, nil
}
