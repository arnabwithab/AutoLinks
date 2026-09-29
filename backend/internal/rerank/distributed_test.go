// ----- distributed graph persistence tests @ backend/internal/rerank/distributed_test.go -----
package rerank

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGraphRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_URL", "redis://"+mr.Addr())
	oldClient := rerankRdb
	rerankRdbOnce = sync.Once{}
	t.Cleanup(func() {
		rerankRdb = oldClient
		rerankRdbOnce = sync.Once{}
		linkGraphMu.Lock()
		linkGraph = make(map[string]int)
		graphCacheExpires = time.Time{}
		linkGraphMu.Unlock()
	})
	return mr
}

func TestMergeRestoreRoundTrip(t *testing.T) {
	mr := setupGraphRedis(t)
	defer mr.Close()

	MergeLinkGraph(map[string]int{"https://a.example/": 3})

	linkGraphMu.Lock()
	linkGraph = make(map[string]int)
	linkGraphMu.Unlock()

	restored := RestoreLinkGraph()
	assert.Equal(t, 3, restored["https://a.example/"])
}

func TestConcurrentMergeNoLostKeys(t *testing.T) {
	mr := setupGraphRedis(t)
	defer mr.Close()

	InitLinkGraph(map[string]int{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			MergeLinkGraph(map[string]int{fmt.Sprintf("https://w%d.example/", i): i})
		}(i)
	}
	wg.Wait()

	restored := RestoreLinkGraph()
	assert.Len(t, restored, 10)
}

func TestStaleCacheRefreshes(t *testing.T) {
	mr := setupGraphRedis(t)
	defer mr.Close()

	InitLinkGraph(map[string]int{"https://old.example/": 1})
	require.NoError(t, getRedisClient().HSet(t.Context(), LinkGraphHashKey, "https://new.example/", 5).Err())

	linkGraphMu.Lock()
	graphCacheExpires = time.Now().Add(-time.Second)
	linkGraphMu.Unlock()

	refreshGraphIfStale()
	assert.Equal(t, 5, GetLinkGraph()["https://new.example/"])
}
