import { useState } from 'react'
import { Settings } from './views/Settings'

const views = ['Movies', 'Series', 'WebDAV', 'Storage', 'Notifications', 'Settings'] as const
type View = (typeof views)[number]

export function App() {
  const [view, setView] = useState<View>('Movies')
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
        {view === 'Settings' ? <Settings /> : <p>{view} — arrives in a later phase.</p>}
      </main>
    </div>
  )
}
