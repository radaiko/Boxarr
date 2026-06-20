import { useEffect, useState } from 'react'
import { Settings } from './views/Settings'
import { Movies } from './views/Movies'
import { Series } from './views/Series'
import { Notifications } from './views/Notifications'
import { Storage } from './views/Storage'
import { WebDAV } from './views/WebDAV'
import { getJSON, loadImageBase } from './api'

const views = ['Movies', 'Series', 'WebDAV', 'Storage', 'Notifications', 'Settings'] as const
type View = (typeof views)[number]

export function App() {
  const [view, setView] = useState<View>('Movies')
  const [unread, setUnread] = useState(0)

  useEffect(() => {
    void loadImageBase()
    const poll = () =>
      getJSON<{ unreadCount: number }>('/notifications/unread-count')
        .then((r) => setUnread(r.unreadCount))
        .catch(() => {})
    void poll()
    const t = setInterval(() => void poll(), 30_000)
    return () => clearInterval(t)
  }, [view])

  return (
    <div>
      <header>
        <h1>Boxarr</h1>
        <nav>
          {views.map((v) => (
            <button key={v} onClick={() => setView(v)} disabled={v === view}>
              {v}
              {v === 'Notifications' && unread > 0 ? ` (${unread})` : ''}
            </button>
          ))}
        </nav>
      </header>
      <main>
        {view === 'Movies' && <Movies />}
        {view === 'Series' && <Series />}
        {view === 'WebDAV' && <WebDAV />}
        {view === 'Storage' && <Storage />}
        {view === 'Notifications' && <Notifications />}
        {view === 'Settings' && <Settings />}
      </main>
    </div>
  )
}
