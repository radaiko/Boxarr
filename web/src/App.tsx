import { useEffect, useRef, useState } from 'react'
import { getJSON, loadImageBase } from './api'
import { Icon } from './ui'
import { Settings } from './views/Settings'
import { Movies } from './views/Movies'
import { Series } from './views/Series'
import { Notifications } from './views/Notifications'
import { TorBox } from './views/TorBox'
import { Activity } from './views/Activity'

const NAV = [
  { id: 'Movies', icon: 'movies', group: 'Library' },
  { id: 'Series', icon: 'series', group: 'Library' },
  { id: 'Anime', icon: 'anime', group: 'Library' },
  { id: 'TorBox', icon: 'storage', group: 'System' },
  { id: 'Activity', icon: 'download', group: 'Activity' },
  { id: 'Notifications', icon: 'notifications', group: 'Activity' },
  { id: 'Settings', icon: 'settings', group: 'Config' },
] as const
type View = (typeof NAV)[number]['id']

interface Status {
  version: string
  counts: { movies: number; series: number; anime: number; activeJobs: number; unreadNotifications: number }
}

export function App() {
  const [view, setView] = useState<View>('Movies')
  const [status, setStatus] = useState<Status | null>(null)
  // Cross-view jump: when a tracked WebDAV item is clicked, open its catalog page.
  const [openTarget, setOpenTarget] = useState<{ view: View; id: number; seq: number } | null>(null)
  const seq = useRef(0)
  function openCatalog(kind: string, id: number) {
    const v: View = kind === 'movie' ? 'Movies' : kind === 'anime' ? 'Anime' : 'Series'
    seq.current += 1
    setOpenTarget({ view: v, id, seq: seq.current })
    setView(v)
  }
  const openFor = (v: View) => (openTarget?.view === v ? openTarget : undefined)

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
    if (id === 'Anime') return counts?.anime
    if (id === 'Activity') return counts?.activeJobs
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
          {view === 'Movies' && <Movies openId={openFor('Movies')?.id} openSeq={openFor('Movies')?.seq} />}
          {view === 'Series' && <Series openId={openFor('Series')?.id} openSeq={openFor('Series')?.seq} />}
          {view === 'Anime' && <Series anime openId={openFor('Anime')?.id} openSeq={openFor('Anime')?.seq} />}
          {view === 'TorBox' && <TorBox onOpenCatalog={openCatalog} />}
          {view === 'Activity' && <Activity />}
          {view === 'Notifications' && <Notifications />}
          {view === 'Settings' && <Settings />}
        </main>
      </div>
    </div>
  )
}
