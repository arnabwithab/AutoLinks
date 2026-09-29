// ----- internal link graph builder @ backend/internal/ingest/linkgraph.go -----
package ingest

import (
	"sort"

	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

// BuildLinkGraph builds inbound link counts by inverting each page's outbound internal links.
func BuildLinkGraph(crawledPages PageMap) map[string]int {
	graph := make(map[string]int, len(crawledPages))
	for url := range crawledPages {
		graph[NormalizeURL(url)] = 0
	}

	skippedTargets := make(map[string]int)
	matchedTargets := 0
	zeroOutboundPages := 0
	totalOutboundLinks := 0

	for sourceURL, pageData := range crawledPages {
		outboundLinks := pageData.OutboundLinks
		totalOutboundLinks += len(outboundLinks)
		if len(outboundLinks) == 0 {
			zeroOutboundPages++
		}

		for _, targetURL := range outboundLinks {
			normalizedTarget := NormalizeURL(targetURL)
			if _, ok := graph[normalizedTarget]; !ok {
				skippedTargets[normalizedTarget]++
				continue
			}
			normalizedSource := NormalizeURL(sourceURL)
			if normalizedTarget == normalizedSource {
				continue
			}
			graph[normalizedTarget]++
			matchedTargets++
		}
	}

	var orphanURLs []string
	for url, inboundCount := range graph {
		if inboundCount == 0 {
			orphanURLs = append(orphanURLs, url)
		}
	}

	type urlCount struct {
		URL   string
		Count int
	}

	var topInbound []urlCount
	for url, count := range graph {
		topInbound = append(topInbound, urlCount{URL: url, Count: count})
	}
	sort.Slice(topInbound, func(i, j int) bool {
		return topInbound[i].Count > topInbound[j].Count
	})
	if len(topInbound) > 5 {
		topInbound = topInbound[:5]
	}

	var unmatchedSamples []urlCount
	for url, count := range skippedTargets {
		unmatchedSamples = append(unmatchedSamples, urlCount{URL: url, Count: count})
	}
	sort.Slice(unmatchedSamples, func(i, j int) bool {
		return unmatchedSamples[i].Count > unmatchedSamples[j].Count
	})
	if len(unmatchedSamples) > 5 {
		unmatchedSamples = unmatchedSamples[:5]
	}

	totalSkipped := 0
	for _, c := range skippedTargets {
		totalSkipped += c
	}

	logger.Info(
		"Link graph summary: urls=%d, total_outbound_links=%d, matched_targets=%d, unmatched_targets=%d, zero_outbound_pages=%d, orphan_urls=%d",
		len(graph),
		totalOutboundLinks,
		matchedTargets,
		totalSkipped,
		zeroOutboundPages,
		len(orphanURLs),
	)

	if len(orphanURLs) > 0 {
		sample := orphanURLs
		if len(sample) > 5 {
			sample = sample[:5]
		}
		logger.Info("Orphan URL sample: %v", sample)
	}

	if len(topInbound) > 0 {
		logger.Info("Top inbound URLs: %v", topInbound)
	}

	if len(unmatchedSamples) > 0 {
		logger.Warning("Internal links skipped because target is outside crawled sitemap set: %v", unmatchedSamples)
	}

	return graph
}
