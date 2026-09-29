import { UserButton } from '@clerk/clerk-react'

const navItems = [
  { id: 'home', label: 'Home' },
  { id: 'workspace', label: 'Workspace' },
  { id: 'sitemap', label: 'Sitemaps' },
]

function Header({ currentView, onNavigate }) {
  return (
    <header className="al-header">
      <button className="al-logo al-logo-btn" onClick={() => onNavigate('home')}>
        AutoLinks
      </button>
      <nav className="al-nav" aria-label="Primary">
        {navItems.map((item) => (
          <button
            key={item.id}
            className={`al-nav-tab ${currentView === item.id ? 'selected' : ''}`}
            onClick={() => onNavigate(item.id)}
          >
            {item.label}
          </button>
        ))}
      </nav>
      <div className="al-user">
        <UserButton
          afterSignOutUrl="/"
          userProfileMode="navigation"
          appearance={{
            elements: {
              userButtonAvatarBox: { width: '2rem', height: '2rem' },
            },
          }}
        />
      </div>
    </header>
  )
}

export default Header
