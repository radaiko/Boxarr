import { useEffect, useState } from 'react'
import { getJSON, loadImageBase } from './api'
import { Icon } from './ui'
import { Settings } from './views/Settings'
import { Movies } from './views/Movies'
import { Series } from './views/Series'
import { Notifications } from './views/Notifications'
import { Storage } from './views/Storage'
import { WebDAV } from './views/WebDAV'

const NAV = [
  { id: 'Movies', icon: 'movies', group: 'Library' },
  { id: 'Series', icon: 'series', group: 'Library' },
  { id: 'WebDAV', icon: 'webdav', group: 'System' },
  { id: 'Storage', icon: 'storage', group: 'System' },
  { id: 'Notifications', icon: 'notifications', group: 'Activity' },
  { id: 'Settings', icon: 'settings', group: 'Config' },
] as const
type View = (typeof NAV)[number]['id']

interface Status {
  version: string
  counts: { movies: number; series: number; activeJobs: number; unreadNotifications: number }
}

export function App() {
  const [view, setView] = useState<View>('Movies')
  const [status, setStatus] = useState<Status | null>(null)

  useEffect(() => {
    void loadImageBase()
    const poll = () => getJSON<Status>('/status').then(setStatus).catch(() => {})
    void poll()
    const t = setInterval(() => void poll(), 30_000)
    return () => clearInterval(t)
  }, [view])

  const counts = status?.counts
  const countFor = (id: View): number | undefined => {
    if (id === 'Movies') return counts?.movies
    if (id === 'Series') return counts?.series
    if (id === 'Notifications') return counts?.unreadNotifications
    return undefined
  }
  const group = NAV.find((n) => n.id === view)?.group ?? ''

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark" />
          <span className="brand-name">BOX<b>ARR</b></span>
        </div>
        <nav className="nav">
          {NAV.map((n) => {
            const c = countFor(n.id)
            const alert = n.id === 'Notifications' && !!c
            return (
              <button key={n.id} className={`nav-item${view === n.id ? ' active' : ''}`}
                onClick={() => setView(n.id)} aria-current={view === n.id}>
                <Icon name={n.icon} />
                <span className="lbl">{n.id}</span>
                {c ? <span className={`nav-count${alert ? ' alert' : ''}`}>{c}</span> : null}
              </button>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <span>{status ? `v${status.version}` : '—'}</span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span className={`dot${status ? ' ok' : ''}`} />{status ? 'online' : 'offline'}
          </span>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <div>
            <p className="eyebrow">{group}</p>
            <h1>{view}</h1>
          </div>
        </header>
        <main className="content">
          {view === 'Movies' && <Movies />}
          {view === 'Series' && <Series />}
          {view === 'WebDAV' && <WebDAV />}
          {view === 'Storage' && <Storage />}
          {view === 'Notifications' && <Notifications />}
          {view === 'Settings' && <Settings />}
        </main>
      </div>
    </div>
  )
}
