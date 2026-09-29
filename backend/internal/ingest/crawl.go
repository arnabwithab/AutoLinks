// ----- sitemap crawl and content extraction @ backend/internal/ingest/crawl.go -----
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	htmlesc "html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/embed"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/arnabwithab/AutoLinks/backend/internal/qdrant"
	qdrantpb "github.com/qdrant/go-client/qdrant"
)

var (
	hrefRE    = regexp.MustCompile(`(?is)<a\s+[^>]*?href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	tagRE     = regexp.MustCompile(`<[^>]*>`)
	spaceRE   = regexp.MustCompile(`\s+`)
	noiseRE   = regexp.MustCompile(`(?is)<(script|style|noscript|template|head|nav|footer|svg)[^>]*>.*?</(?:script|style|noscript|template|head|nav|footer|svg)>`)
	commentRE = regexp.MustCompile(`(?s)<!--.*?-->`)
)

const (
	maxSitemapDepth = 5
	maxSitemapURLs  = 50000
)

// PageData holds links extracted from a crawled page.
type PageData struct {
	OutboundLinks []string
}

// PageMap maps normalized URLs to their extracted page data.
type PageMap map[string]*PageData

// NormalizeURL normalizes URLs so sitemap entries and extracted links compare consistently.
// Query strings are intentionally dropped so tracking params and pagination collapse to the
// canonical path; this keeps the link graph stable but merges ?page=2 into its base page.
func NormalizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}

	return fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, path)
}

// ExtractInternalLinks extracts normalized internal links from article HTML.
func ExtractInternalLinks(htmlStr, baseURL string) []string {
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	domain := parsedBase.Host
	sourceURL := NormalizeURL(baseURL)

	seen := make(map[string]bool)
	var links []string

	matches := hrefRE.FindAllStringSubmatch(htmlStr, -1)
	for _, match := range matches {
		href := match[1]
		if href == "" {
			href = match[2]
		}
		if href == "" {
			href = match[3]
		}
		if href == "" {
			continue
		}
		fullURL, err := resolveURL(baseURL, href)
		if err != nil {
			continue
		}

		parsedLink, err := url.Parse(fullURL)
		if err != nil {
			continue
		}
		if parsedLink.Scheme != "http" && parsedLink.Scheme != "https" {
			continue
		}
		if parsedLink.Host != domain {
			continue
		}

		normalized := NormalizeURL(fullURL)
		if normalized == sourceURL {
			continue
		}
		if seen[normalized] {
			continue
		}

		seen[normalized] = true
		links = append(links, normalized)
	}

	sort.Strings(links)
	return links
}

func resolveURL(base, ref string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(refURL).String(), nil
}

func extractTextFromHTML(htmlStr string) string {
	text := commentRE.ReplaceAllString(htmlStr, " ")
	text = noiseRE.ReplaceAllString(text, " ")
	text = tagRE.ReplaceAllString(text, " ")
	text = htmlesc.UnescapeString(text)
	text = spaceRE.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// FetchAndExtract fetches a URL, returns normalized URL, text, html, and error.
func FetchAndExtract(rawURL string) (string, string, string, error) {
	resp, err := safeGet(rawURL)
	if err != nil {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("fetch returned %d", resp.StatusCode)
	}

	htmlBytes, err := readLimited(resp.Body)
	if err != nil {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("read body failed: %w", err)
	}
	htmlStr := string(htmlBytes)

	text := extractTextFromHTML(htmlStr)
	if text == "" {
		logger.Warning("No text extracted from %s", rawURL)
	}

	return NormalizeURL(rawURL), text, htmlStr, nil
}

// ParseSitemap parses a sitemap XML (including sitemap indexes) and extracts all article URLs.
func ParseSitemap(sitemapURL string) []string {
	return parseSitemap(sitemapURL, 0, map[string]bool{})
}

func parseSitemap(sitemapURL string, depth int, visited map[string]bool) []string {
	if depth > maxSitemapDepth {
		logger.Warning("Sitemap recursion depth exceeded at %s", sitemapURL)
		return nil
	}
	if visited[sitemapURL] {
		logger.Warning("Skipping already-visited sitemap %s", sitemapURL)
		return nil
	}
	visited[sitemapURL] = true

	resp, err := safeGet(sitemapURL)
	if err != nil {
		logger.Error("Sitemap parse error: %s", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("Sitemap returned %d for %s", resp.StatusCode, sitemapURL)
		return nil
	}

	body, err := readLimited(resp.Body)
	if err != nil {
		logger.Error("Sitemap read error: %s", err)
		return nil
	}

	type urlElement struct {
		Loc string `xml:"loc"`
	}

	type URLSet struct {
		XMLName xml.Name     `xml:"urlset"`
		URLs    []urlElement `xml:"url"`
	}

	type SitemapIndex struct {
		XMLName  xml.Name     `xml:"sitemapindex"`
		Sitemaps []urlElement `xml:"sitemap"`
	}

	var index SitemapIndex
	if err := xml.Unmarshal(body, &index); err == nil && len(index.Sitemaps) > 0 {
		var allURLs []string
		for _, sm := range index.Sitemaps {
			if sm.Loc == "" || len(allURLs) >= maxSitemapURLs {
				continue
			}
			allURLs = append(allURLs, parseSitemap(sm.Loc, depth+1, visited)...)
		}
		return allURLs
	}

	var urlSet URLSet
	if err := xml.Unmarshal(body, &urlSet); err != nil {
		logger.Error("Sitemap XML parse error: %s", err)
		return nil
	}

	var urls []string
	for _, u := range urlSet.URLs {
		if u.Loc == "" {
			continue
		}
		urls = append(urls, u.Loc)
		if len(urls) >= maxSitemapURLs {
			logger.Warning("Sitemap URL cap reached (%d)", maxSitemapURLs)
			break
		}
	}
	return urls
}

// UpsertChunks upserts chunk embeddings to Qdrant.
func UpsertChunks(articleURL string, chunks []string, embeddings [][]float64) error {
	client, err := qdrant.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get qdrant client: %w", err)
	}

	collectionName := config.QdrantCollection()

	var points []*qdrantpb.PointStruct
	for i, chunk := range chunks {
		hashInput := fmt.Sprintf("%s_%d", articleURL, i)
		hash := sha256.Sum256([]byte(hashInput))
		pointID := uint64(0)
		for j := 0; j < 8; j++ {
			pointID = (pointID << 8) | uint64(hash[j])
		}

		vector32 := make([]float32, len(embeddings[i]))
		for j, v := range embeddings[i] {
			vector32[j] = float32(v)
		}

		payload := map[string]interface{}{
			"url":         articleURL,
			"chunk_text":  chunk,
			"chunk_index": float64(i),
		}

		points = append(points, &qdrantpb.PointStruct{
			Id:      qdrantpb.NewIDNum(pointID),
			Vectors: qdrantpb.NewVectors(vector32...),
			Payload: qdrantpb.NewValueMap(payload),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := qdrant.DeletePointsByURL(ctx, articleURL); err != nil {
		return fmt.Errorf("failed to clear stale points for %s: %w", articleURL, err)
	}

	req := &qdrantpb.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
	}

	_, err = client.Upsert(ctx, req)
	if err != nil {
		return fmt.Errorf("qdrant upsert failed: %w", err)
	}

	return nil
}

// IngestArticle chunks text, generates embeddings, and upserts to Qdrant.
// It returns the number of chunks ingested.
func IngestArticle(rawURL string, text string) (int, error) {
	normalizedURL := NormalizeURL(rawURL)

	chunks := ChunkText(text, 5)
	if len(chunks) == 0 {
		logger.Warning("No chunks generated for %s", normalizedURL)
		return 0, nil
	}

	embeddings, err := embed.EmbedBatch(chunks)
	if err != nil {
		return 0, fmt.Errorf("embed batch failed: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return 0, fmt.Errorf("embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	if err := UpsertChunks(normalizedURL, chunks, embeddings); err != nil {
		return 0, fmt.Errorf("upsert failed: %w", err)
	}

	logger.Info("Ingested %d chunks for %s", len(chunks), normalizedURL)
	return len(chunks), nil
}

// StreamFetchEmbedUpsert fetches a single page, embeds it, and upserts to Qdrant.
// Returns the page's outbound internal links (for link graph building) and an error
// if the page could not be processed.
func StreamFetchEmbedUpsert(rawURL string) ([]string, error) {
	normalizedURL, text, htmlStr, err := FetchAndExtract(rawURL)
	if err != nil {
		logger.Warning("Failed to stream ingest %s: %s", rawURL, err)
		return nil, err
	}
	if text == "" {
		return nil, fmt.Errorf("no text extracted")
	}

	outboundLinks := ExtractInternalLinks(htmlStr, normalizedURL)

	if _, err := IngestArticle(normalizedURL, text); err != nil {
		logger.Warning("Failed to stream ingest %s: %s", rawURL, err)
		return nil, err
	}

	logger.Info("Stream ingested %s", normalizedURL)
	return outboundLinks, nil
}
