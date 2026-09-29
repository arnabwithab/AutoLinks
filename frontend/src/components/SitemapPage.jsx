import { useEffect, useRef, useState } from 'react'
import { useAuth } from '@clerk/clerk-react'
import { fetchSitemapStatus, ingestSitemap, fetchJobStatus } from '../services/api'

function SitemapPage() {
  const [sitemapUrl, setSitemapUrl] = useState('')
  const [statusLoading, setStatusLoading] = useState(true)
  const [statusError, setStatusError] = useState('')
  const [hasSitemap, setHasSitemap] = useState(false)
  const [urlCount, setUrlCount] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [submitMessage, setSubmitMessage] = useState('')
  const { getToken } = useAuth()
  const pollRef = useRef(null)
  const mountedRef = useRef(true)

  const loadStatus = async () => {
    setStatusLoading(true)
    setStatusError('')

    try {
      const result = await fetchSitemapStatus(getToken)
      setHasSitemap(result.hasSitemap)
      setUrlCount(result.urlCount)
    } catch (err) {
      setStatusError(err.message)
      setHasSitemap(false)
      setUrlCount(0)
    } finally {
      setStatusLoading(false)
    }
  }

  useEffect(() => {
    loadStatus()
    return () => {
      mountedRef.current = false
      if (pollRef.current) clearTimeout(pollRef.current)
    }
  }, [])

  const pollJob = (jobId) => {
    pollRef.current = setTimeout(async () => {
      try {
        const job = await fetchJobStatus(jobId, getToken)
        if (!mountedRef.current) return

        if (job.status === 'done' || job.status === 'failed') {
          setSubmitMessage(
            job.status === 'done'
              ? `Crawl complete. Indexed ${job.articlesDone} of ${job.total} pages.`
              : `Crawl failed after ${job.articlesDone} of ${job.total} pages.`,
          )
          if (job.errors?.length) setSubmitError(job.errors.join('; '))
          setSubmitting(false)
          await loadStatus()
          return
        }

        setSubmitMessage(`Crawling... ${job.progressPct}% (${job.articlesDone}/${job.total})`)
        pollJob(jobId)
      } catch (err) {
        if (!mountedRef.current) return
        setSubmitError(err.message)
        setSubmitting(false)
      }
    }, 2000)
  }

  const handleSubmit = async (e) => {
    e.preventDefault()

    if (!sitemapUrl.trim()) {
      setSubmitError('Add a sitemap XML URL before starting a crawl.')
      return
    }

    setSubmitting(true)
    setSubmitError('')
    setSubmitMessage('Starting crawl...')

    try {
      const result = await ingestSitemap(sitemapUrl.trim(), 5, getToken)
      if (!result.jobId) {
        setSubmitMessage('Crawl queued.')
        setSubmitting(false)
        await loadStatus()
        return
      }
      setSubmitMessage('Crawl queued. Waiting for progress...')
      pollJob(result.jobId)
    } catch (err) {
      setSubmitError(err.message)
      setSubmitting(false)
    }
  }

  return (
    <div className="al-page al-sitemap-page">
      <section className="al-sitemap-hero">
        <div className="al-section-label">Sitemap Control</div>
        <h1 className="al-sitemap-title">Check whether a sitemap XML is loaded, then submit a new one to crawl.</h1>
        <p className="al-sitemap-copy">
          This page uses the current indexed link graph as the status signal. If URLs have already
          been loaded into the crawler, AutoLinks treats that as an active sitemap-backed corpus.
        </p>
      </section>

      <div className="al-sitemap-grid">
        <section className="al-surface-card al-sitemap-status-card">
          <div className="al-section-label">Current Status</div>
          {statusLoading ? (
            <p className="al-sitemap-note">Checking the crawler state...</p>
          ) : statusError ? (
            <div className="al-sitemap-feedback error">{statusError}</div>
          ) : (
            <>
              <div className={`al-sitemap-pill ${hasSitemap ? 'ready' : 'empty'}`}>
                {hasSitemap ? 'XML loaded' : 'No XML loaded'}
              </div>
              <p className="al-sitemap-metric">
                {hasSitemap
                  ? `${urlCount} indexed URLs are currently available for semantic search.`
                  : 'No indexed URLs are available yet, so the crawler has not loaded a sitemap-backed corpus.'}
              </p>
              <button
                className="al-cta al-secondary-cta"
                type="button"
                onClick={loadStatus}
              >
                Refresh Status
              </button>
            </>
          )}
        </section>

        <section className="al-surface-card al-sitemap-form-card">
          <div className="al-section-label">Upload New XML</div>
          <h2 className="al-sitemap-form-title">Point AutoLinks at a sitemap XML and kick off a fresh crawl.</h2>
          <form className="al-sitemap-form" onSubmit={handleSubmit}>
            <label className="al-sitemap-label" htmlFor="sitemap-url">
              Sitemap XML URL
            </label>
            <input
              id="sitemap-url"
              className="al-sitemap-input"
              type="url"
              value={sitemapUrl}
              onChange={(e) => setSitemapUrl(e.target.value)}
              placeholder="https://example.com/post-sitemap.xml"
            />
            <button
              className="al-cta al-primary-cta"
              type="submit"
              disabled={submitting}
            >
              {submitting ? 'Starting Crawl...' : 'Crawl New XML'}
            </button>
          </form>

          {submitMessage ? <div className="al-sitemap-feedback success">{submitMessage}</div> : null}
          {submitError ? <div className="al-sitemap-feedback error">{submitError}</div> : null}
        </section>
      </div>
    </div>
  )
}

export default SitemapPage
