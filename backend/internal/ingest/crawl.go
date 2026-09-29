// ----- sitemap crawl and content extraction @ backend/internal/ingest/crawl.go -----
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
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
	"golang.org/x/sync/semaphore"
)

var hrefRE = regexp.MustCompile(`<a\s+[^>]*href=["']([^"']+)["'][^>]*>`)
var tagRE = regexp.MustCompile(`<[^>]*>`)
var spaceRE = regexp.MustCompile(`\s+`)

// PageData holds extracted page content and outbound links.
type PageData struct {
	Text          string
	HTML          string
	OutboundLinks []string
}

// PageMap maps normalized URLs to their extracted page data.
type PageMap map[string]*PageData

// NormalizeURL normalizes URLs so sitemap entries and extracted links compare consistently.
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
	text := tagRE.ReplaceAllString(htmlStr, " ")
	text = spaceRE.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// FetchAndExtract fetches a URL, returns normalized URL, text, html, and error.
func FetchAndExtract(rawURL string) (string, string, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("fetch returned %d", resp.StatusCode)
	}

	htmlBytes, err := io.ReadAll(resp.Body)
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

// FetchAndExtractConcurrent fetches URL and extracts text with semaphore-bounded concurrency.
func FetchAndExtractConcurrent(rawURL string, sem *semaphore.Weighted) (normalizedURL string, text string, html string, err error) {
	ctx := context.Background()
	if err := sem.Acquire(ctx, 1); err != nil {
		return NormalizeURL(rawURL), "", "", fmt.Errorf("semaphore acquire: %w", err)
	}
	defer sem.Release(1)

	return FetchAndExtract(rawURL)
}

// CrawlAndExtractBulk crawls URLs concurrently and returns extracted text plus internal links.
func CrawlAndExtractBulk(urls []string, maxConcurrent int64) PageMap {
	sem := semaphore.NewWeighted(maxConcurrent)
	results := make(PageMap)

	type result struct {
		url  string
		data *PageData
	}

	ch := make(chan result, len(urls))

	for _, rawURL := range urls {
		go func(u string) {
			normalizedURL, text, html, err := FetchAndExtractConcurrent(u, sem)
			if err != nil {
				logger.Warning("Failed to fetch %s: %s", u, err)
				ch <- result{url: normalizedURL}
				return
			}
			if text == "" {
				ch <- result{url: normalizedURL}
				return
			}

			links := ExtractInternalLinks(html, normalizedURL)
			ch <- result{
				url: normalizedURL,
				data: &PageData{
					Text:          text,
					HTML:          html,
					OutboundLinks: links,
				},
			}
		}(rawURL)
	}

	for i := 0; i < len(urls); i++ {
		r := <-ch
		if r.data != nil {
			results[r.url] = r.data
		}
	}

	return results
}

// ParseSitemap parses a sitemap XML and extracts all article URLs.
func ParseSitemap(sitemapURL string) []string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sitemapURL)
	if err != nil {
		logger.Error("Sitemap parse error: %s", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
			urls := ParseSitemap(sm.Loc)
			allURLs = append(allURLs, urls...)
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
		if u.Loc != "" {
			urls = append(urls, u.Loc)
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
func IngestArticle(rawURL string, text string) error {
	normalizedURL := NormalizeURL(rawURL)

	chunks := ChunkText(text, 5)
	if len(chunks) == 0 {
		logger.Warning("No chunks generated for %s", normalizedURL)
		return nil
	}

	embeddings, err := embed.EmbedBatch(chunks)
	if err != nil {
		return fmt.Errorf("embed batch failed: %w", err)
	}

	err = UpsertChunks(normalizedURL, chunks, embeddings)
	if err != nil {
		return fmt.Errorf("upsert failed: %w", err)
	}

	logger.Info("Ingested %d chunks for %s", len(chunks), normalizedURL)
	return nil
}

// StreamFetchEmbedUpsert fetches a single page, embeds it, and upserts to Qdrant.
// Returns the page's outbound internal links (for link graph building),
// or nil if the page could not be processed.
func StreamFetchEmbedUpsert(rawURL string) []string {
	normalizedURL, text, html, err := FetchAndExtract(rawURL)
	if err != nil || text == "" {
		if err != nil {
			logger.Warning("Failed to stream ingest %s: %s", rawURL, err)
		}
		return nil
	}

	outboundLinks := ExtractInternalLinks(html, normalizedURL)

	if err := IngestArticle(normalizedURL, text); err != nil {
		logger.Warning("Failed to stream ingest %s: %s", rawURL, err)
		return nil
	}

	logger.Info("Stream ingested %s", normalizedURL)
	return outboundLinks
}
