export function buildHighlightedHtml(draftText, recommendations) {
  const escaped = escapeHtml(draftText)
  if (!recommendations?.length) {
    return escaped
  }

  const phrases = [...new Set(recommendations.map((item) => item.exact_phrase).filter(Boolean))]
  if (!phrases.length) {
    return escaped
  }

  const keyByEscapedLower = new Map()
  phrases.forEach((phrase) => {
    keyByEscapedLower.set(escapeHtml(phrase).toLowerCase(), encodePhraseKey(phrase))
  })

  const pattern = phrases
    .map((phrase) => escapeRegex(escapeHtml(phrase)))
    .sort((a, b) => b.length - a.length)
    .join('|')
  const regex = new RegExp(`(${pattern})`, 'gi')

  return escaped.replace(regex, (match) => {
    const phraseKey = keyByEscapedLower.get(match.toLowerCase())
    if (!phraseKey) {
      return match
    }
    return `<mark class="hl" data-phrase-key="${phraseKey}">${match}</mark>`
  })
}

export function encodePhraseKey(phrase) {
  return encodeURIComponent(phrase)
}

function escapeHtml(text) {
  const map = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;',
  }

  return text.replace(/[&<>"']/g, (match) => map[match])
}

function escapeRegex(text) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
