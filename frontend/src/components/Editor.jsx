import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/clerk-react'
import { useStore } from '../store/store'
import { fetchRecommendations } from '../services/api'
import { buildHighlightedHtml, encodePhraseKey } from '../utils/editor_highlight'

function Editor() {
  const [showHighlight, setShowHighlight] = useState(false)
  const textareaRef = useRef(null)
  const highlightRef = useRef(null)
  const requestIdRef = useRef(0)
  const { getToken } = useAuth()
  const {
    draftText,
    setDraftText,
    recommendations,
    loading,
    setLoading,
    setRecommendations,
    setError,
    activeCardId,
  } = useStore()

  const handleAnalyze = async () => {
    if (!draftText.trim()) return
    const requestId = ++requestIdRef.current
    setLoading(true)
    setError(null)
    try {
      const result = await fetchRecommendations(draftText, 0.7, 0.65, getToken)
      if (requestId !== requestIdRef.current) return
      setRecommendations(result.recommendations, result.latency)
      setShowHighlight(true)
    } catch (err) {
      if (requestId !== requestIdRef.current) return
      setError(err.message)
      setLoading(false)
    }
  }

  const handleTextChange = (e) => {
    requestIdRef.current += 1
    setDraftText(e.target.value)
    setShowHighlight(false)
  }

  const handleEdit = () => {
    setShowHighlight(false)
  }

  useEffect(() => {
    if (!showHighlight || activeCardId === null || !highlightRef.current) {
      return
    }

    const recommendation = recommendations[activeCardId]
    if (!recommendation) {
      return
    }

    const phraseKey = encodePhraseKey(recommendation.exact_phrase)
    const marks = highlightRef.current.querySelectorAll(
      `mark.hl[data-phrase-key="${phraseKey}"]`,
    )

    if (!marks.length) {
      return
    }

    const firstMark = marks[0]
    firstMark.scrollIntoView({
      behavior: 'smooth',
      block: 'center',
      inline: 'nearest',
    })

    marks.forEach((mark) => {
      mark.classList.add('pulse')
      window.setTimeout(() => {
        mark.classList.remove('pulse')
      }, 1400)
    })
  }, [activeCardId, recommendations, showHighlight])

  return (
    <div className="al-ep">
      <div className="al-section-label">Draft Editor</div>
      <div className="al-editor-wrapper">
        {showHighlight && recommendations.length > 0 ? (
          <div
            ref={highlightRef}
            className="al-editor-highlight"
            dangerouslySetInnerHTML={{ __html: buildHighlightedHtml(draftText, recommendations) }}
          />
        ) : (
          <textarea
            ref={textareaRef}
            className="al-editor"
            value={draftText}
            onChange={handleTextChange}
            placeholder="Paste your draft text here to analyze for internal linking opportunities..."
          />
        )}
      </div>
      <button
        className="al-analyze"
        onClick={handleAnalyze}
        disabled={loading || !draftText.trim()}
      >
        {loading ? (
          <>
            <span className="al-spinner" />
            Analyzing...
          </>
        ) : (
          'Analyze'
        )}
      </button>
      {showHighlight && (
        <button className="al-edit-toggle" type="button" onClick={handleEdit}>
          Back to editing
        </button>
      )}
    </div>
  )
}

export default Editor
