import { useStore } from '../store/store'

function Card({ recommendation, index }) {
  const { activeCardId, setActiveCard } = useStore()

  const handleClick = () => {
    if (activeCardId === index) {
      setActiveCard(null)
    } else {
      setActiveCard(index)
    }
  }

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      handleClick()
    }
  }

  const isActive = activeCardId === index

  return (
    <div
      className={`al-card ${isActive ? 'on' : ''}`}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
    >
      <div className="al-card-phrase">{recommendation.exact_phrase}</div>
      <div className="al-card-context">{recommendation.context_snippet}</div>
      <a
        href={safeHref(recommendation.suggested_url)}
        className="al-card-url"
        target="_blank"
        rel="noopener noreferrer"
        onClick={(e) => e.stopPropagation()}
      >
        {truncateUrl(recommendation.suggested_url)}
      </a>
      <div className="al-card-scores">
        <span className="al-card-score">
          <span className="label">Match:</span>
          <span className="value">{Number(recommendation.similarity_score ?? 0).toFixed(2)}</span>
        </span>
        <span className="al-card-score equity">
          <span className="label">Equity:</span>
          <span className="value">{Number(recommendation.equity_need_score ?? 0).toFixed(2)}</span>
        </span>
      </div>
    </div>
  )
}

function safeHref(url) {
  try {
    const parsed = new URL(url)
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      return parsed.href
    }
  } catch {
    // fall through
  }
  return '#'
}

function truncateUrl(url) {
  try {
    const parsed = new URL(url)
    const path = parsed.pathname + parsed.hash
    if (path.length > 40) {
      return '...' + path.slice(-37)
    }
    return url
  } catch {
    return url.slice(0, 40) + (url.length > 40 ? '...' : '')
  }
}

export default Card
