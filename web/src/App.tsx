import { useEffect, useState } from 'react'
import { Settings } from './views/Settings'
import { Movies } from './views/Movies'
import { Series } from './views/Series'
import { loadImageBase } from './api'

const views = ['Movies', 'Series', 'WebDAV', 'Storage', 'Notifications', 'Settings'] as const
type View = (typeof views)[number]

export function App() {
  const [view, setView] = useState<View>('Movies')
  useEffect(() => { void loadImageBase() }, [])
  return (
    <div>
      <header>
        <h1>Boxarr</h1>
        <nav>
          {views.map((v) => (
            <button key={v} onClick={() => setView(v)} disabled={v === view}>
              {v}
            </button>
          ))}
        </nav>
      </header>
      <main>
        {view === 'Movies' && <Movies />}
        {view === 'Series' && <Series />}
        {view === 'Settings' && <Settings />}
        {view !== 'Movies' && view !== 'Series' && view !== 'Settings' && (
          <p>{view} — arrives in a later phase.</p>
        )}
      </main>
    </div>
  )
}
