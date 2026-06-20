import { useEffect, useState } from 'react'
import {
  getJSON, postJSON, posterURL,
  type Series, type SeriesCandidate, type ListResponse,
} from '../api'
import { SeriesDetail } from './SeriesDetail'

export function Series() {
  const [series, setSeries] = useState<Series[]>([])
  const [selected, setSelected] = useState<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState('')

  function reload() {
    getJSON<ListResponse<Series>>('/series')
      .then((r) => setSeries(r.items))
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  if (err) return <p>Error: {err}</p>
  if (selected !== null) {
    return <SeriesDetail id={selected} onBack={() => { setSelected(null); reload() }} />
  }
  if (adding) {
    return <AddSeries onDone={() => { setAdding(false); reload() }} />
  }

  return (
    <section>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2>Series ({series.length})</h2>
        <button onClick={() => setAdding(true)}>+ Add series</button>
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
        {series.map((s) => (
          <button key={s.id} onClick={() => setSelected(s.id)} style={{ width: 160, textAlign: 'left' }}>
            {s.posterPath ? (
              <img src={posterURL(s.posterPath)} alt={s.title} width={154} height={231} />
            ) : (
              <div style={{ width: 154, height: 231, background: '#222' }} />
            )}
            <div>{s.title} ({s.year})</div>
            <small>{s.status}</small>
          </button>
        ))}
      </div>
    </section>
  )
}

function AddSeries({ onDone }: { onDone: () => void }) {
  const [term, setTerm] = useState('')
  const [results, setResults] = useState<SeriesCandidate[]>([])
  const [busy, setBusy] = useState(false)

  async function search() {
    setBusy(true)
    try {
      const r = await getJSON<{ items: SeriesCandidate[] }>(`/series/lookup?term=${encodeURIComponent(term)}`)
      setResults(r.items)
    } finally {
      setBusy(false)
    }
  }
  async function add(c: SeriesCandidate) {
    await postJSON('/series', { tmdbId: c.tmdbId })
    onDone()
  }

  return (
    <section>
      <button onClick={onDone}>&larr; Back</button>
      <h2>Add series</h2>
      <input value={term} onChange={(e) => setTerm(e.target.value)} placeholder="title…"
        onKeyDown={(e) => e.key === 'Enter' && void search()} />
      <button onClick={() => void search()} disabled={busy || !term}>Search</button>
      <ul>
        {results.map((c) => (
          <li key={c.tmdbId}>
            {c.title} ({c.year}){' '}
            {c.inLibrary ? <em>added</em> : <button onClick={() => void add(c)}>Add</button>}
          </li>
        ))}
      </ul>
    </section>
  )
}
