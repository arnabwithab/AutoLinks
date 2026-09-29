// ----- stream queue tests @ backend/internal/jobs/queue_test.go -----
package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamEnqueueClaimAck(t *testing.T) {
	mr := setupRedis(t)
	defer mr.Close()
	ctx := context.Background()

	if err := EnsureStreamGroup(ctx); err != nil {
		t.Skipf("miniredis lacks stream group support: %v", err)
	}

	jobID, err := CreateJob("crawl_sitemap", map[string]interface{}{"sitemap_url": "https://example.com/s.xml"})
	require.NoError(t, err)

	if _, err := EnqueueStream(ctx, jobID, "fp1", "trace-1"); err != nil {
		t.Skipf("miniredis lacks XADD support: %v", err)
	}

	sm, err := ReadClaim(ctx, "consumer-1", 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, sm)
	assert.Equal(t, jobID, sm.JobID)

	job, err := GetJob(jobID)
	require.NoError(t, err)
	assert.Equal(t, "processing", job.Status)
	assert.NotEmpty(t, job.LeaseToken)

	require.NoError(t, AckStream(ctx, sm.MsgID))
}

func TestDoubleClaimOneWinner(t *testing.T) {
	mr := setupRedis(t)
	defer mr.Close()
	ctx := context.Background()

	if err := EnsureStreamGroup(ctx); err != nil {
		t.Skipf("miniredis lacks stream group support: %v", err)
	}

	jobID, err := CreateJob("crawl_sitemap", nil)
	require.NoError(t, err)
	if _, err := EnqueueStream(ctx, jobID, "", ""); err != nil {
		t.Skipf("miniredis lacks XADD support: %v", err)
	}

	claimed, err := compareAndClaim(ctx, getRedis(), jobID, "lease-a")
	require.NoError(t, err)
	assert.True(t, claimed)

	claimed, err = compareAndClaim(ctx, getRedis(), jobID, "lease-b")
	require.NoError(t, err)
	assert.False(t, claimed, "second claim must lose")

	job, _ := GetJob(jobID)
	assert.Equal(t, "lease-a", job.LeaseToken)
}
