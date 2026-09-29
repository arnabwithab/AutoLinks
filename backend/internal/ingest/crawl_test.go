// ----- crawl and extraction tests @ backend/internal/ingest/crawl_test.go -----
package ingest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTextFromHTMLStripsNoise(t *testing.T) {
	html := `<html><head><title>Ignored title</title></head>
<body>
<nav>Home About Contact</nav>
<script>var tracking = "NAVSCRIPT";</script>
<style>.x { color: red }</style>
<article>
<p>Internal linking distributes authority across a site. Orphan pages receive nothing until they are linked.</p>
<p>Equity-aware ranking favours pages with few inbound links so the graph stays healthy over time.</p>
</article>
<footer>Copyright 2026</footer>
</body></html>`

	text := extractTextFromHTML(html, "https://example.com/post")

	assert.Contains(t, text, "Internal linking distributes authority")
	assert.Contains(t, text, "Equity-aware ranking favours")
	assert.NotContains(t, text, "NAVSCRIPT")
	assert.NotContains(t, text, "Home About Contact")
	assert.NotContains(t, text, "Copyright 2026")
}

func TestExtractInternalLinksHandlesUnquotedAndJS(t *testing.T) {
	html := `<a href=/one>one</a>
<a href='https://example.com/two'>two</a>
<a href="https://other.com/x">external</a>
<a href="javascript:void(0)">js</a>
<a href="#">fragment</a>
<a href="mailto:hi@example.com">mail</a>`

	links := ExtractInternalLinks(html, "https://example.com")
	assert.Equal(t, []string{"https://example.com/one", "https://example.com/two"}, links)
}
