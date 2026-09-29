// ----- fingerprint tests @ backend/internal/jobs/fingerprint_test.go -----
package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFingerprintStableAcrossVariants(t *testing.T) {
	a := FingerprintURL("https://example.com/sitemap.xml")
	b := FingerprintURL("https://example.com/sitemap.xml?utm_source=x")
	c := FingerprintURL("https://example.com/sitemap.xml/")
	assert.Equal(t, a, b)
	assert.Equal(t, a, c)
	assert.NotEmpty(t, a)
}

func TestCreateJobDeduped(t *testing.T) {
	mr := setupRedis(t)
	defer mr.Close()

	fp := FingerprintURL("https://example.com/sitemap.xml")
	id1, created1, err := CreateJobDeduped("crawl_sitemap", map[string]interface{}{"sitemap_url": "https://example.com/sitemap.xml"}, fp, "trace-1")
	require.NoError(t, err)
	assert.True(t, created1)
	assert.NotEmpty(t, id1)

	id2, created2, err := CreateJobDeduped("crawl_sitemap", map[string]interface{}{"sitemap_url": "https://example.com/sitemap.xml"}, fp, "trace-2")
	require.NoError(t, err)
	assert.False(t, created2)
	assert.Equal(t, id1, id2)
}
