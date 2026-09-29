import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildApiUrl,
  fetchSitemapStatus,
  ingestSitemap,
  fetchJobStatus,
} from './api'

describe('sitemap api helpers', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('builds api urls from the configured frontend env', () => {
    expect(buildApiUrl('/health', { VITE_API_BASE_URL: 'http://localhost:9000/api/v1/' })).toBe(
      'http://localhost:9000/api/v1/health',
    )
  })

  it('maps link graph data into sitemap status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ status: 'success', url_count: 12 }),
      }),
    )

    const result = await fetchSitemapStatus()

    expect(fetch).toHaveBeenCalledWith(
      'https://autolinks.onrender.com/api/v1/link-graph',
      expect.objectContaining({ headers: { 'Content-Type': 'application/json' } }),
    )
    expect(result).toEqual({
      hasSitemap: true,
      urlCount: 12,
    })
  })

  it('posts the sitemap url for ingestion and returns the job id', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ status: 'queued', job_id: 'job-123' }),
      }),
    )

    const result = await ingestSitemap('https://example.com/post-sitemap.xml', 7)

    expect(fetch).toHaveBeenCalledWith(
      'https://autolinks.onrender.com/api/v1/ingest/sitemap',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sitemap_url: 'https://example.com/post-sitemap.xml',
          max_concurrent: 7,
        }),
      }),
    )
    expect(result).toEqual({
      status: 'queued',
      jobId: 'job-123',
    })
  })

  it('maps job status progress', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ status: 'processing', progress_pct: 42.5, articles_done: 17, total: 40 }),
      }),
    )

    const result = await fetchJobStatus('job-123')

    expect(result).toEqual({
      status: 'processing',
      progressPct: 42.5,
      articlesDone: 17,
      total: 40,
      errors: [],
    })
  })
})
