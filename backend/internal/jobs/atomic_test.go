// ----- atomic update tests @ backend/internal/jobs/atomic_test.go -----
package jobs

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncrementProgress(t *testing.T) {
	mr := setupRedis(t)
	defer mr.Close()

	jobID, err := CreateJob("test", nil)
	require.NoError(t, err)

	require.NoError(t, IncrementProgress(jobID, 3))
	require.NoError(t, IncrementProgress(jobID, 2))

	job, err := GetJob(jobID)
	require.NoError(t, err)
	assert.Equal(t, 5, job.ArticlesDone)
}

func TestConcurrentProgressNoLostUpdates(t *testing.T) {
	mr := setupRedis(t)
	defer mr.Close()

	jobID, err := CreateJob("test", nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := IncrementProgress(jobID, 1); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Empty(t, errs)

	job, err := GetJob(jobID)
	require.NoError(t, err)
	assert.Equal(t, 10, job.ArticlesDone)
}
