import { create } from 'zustand'

const getInitialTheme = () => {
  if (typeof window === 'undefined') return 'light'
  const stored = localStorage.getItem('autolinks-theme')
  if (stored) return stored
  return 'system'
}

const applyTheme = (theme) => {
  if (typeof window === 'undefined') return

  let effectiveTheme = theme
  if (theme === 'system') {
    effectiveTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }

  document.documentElement.setAttribute('data-theme', effectiveTheme)
}

export const useStore = create((set) => ({
  draftText: '',
  recommendations: [],
  loading: false,
  error: null,
  latency: null,
  activeCardId: null,

  theme: getInitialTheme(),

  setDraftText: (text) => set({ draftText: text }),

  setTheme: (theme) => {
    localStorage.setItem('autolinks-theme', theme)
    applyTheme(theme)
    set({ theme })
  },

  setActiveCard: (id) => set({ activeCardId: id }),

  setLoading: (loading) => set({ loading }),

  setError: (error) => set({ error }),

  setRecommendations: (recommendations, latency) => set({
    recommendations,
    latency,
    loading: false,
    error: null,
    activeCardId: null,
  }),
}))

if (typeof window !== 'undefined') {
  applyTheme(getInitialTheme())
}
