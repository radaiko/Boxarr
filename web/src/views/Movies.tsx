import { useEffect, useState } from 'react'
import { getJSON, postJSON, posterURL, type Movie, type MovieCandidate, type ListResponse } from '../api'
import { MovieDetail } from './MovieDetail'

export function Movies() {
  const [movies, setMovies] = useState<Movie[]>([])
  const [selected, setSelected] = useState<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState('')

  function reload() {
    getJSON<ListResponse<Movie>>('/movies')
      .then((r) => setMovies(r.items))
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  if (err) return <p>Error: {err}</p>
  if (selected !== null) {
    return <MovieDetail id={selected} onBack={() => { setSelected(null); reload() }} />
  }
  if (adding) {
    return <AddMovie onDone={() => { setAdding(false); reload() }} />
  }

  return (
    <section>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2>Movies ({movies.length})</h2>
        <button onClick={() => setAdding(true)}>+ Add movie</button>
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
        {movies.map((m) => (
          <button key={m.id} onClick={() => setSelected(m.id)} style={{ width: 160, textAlign: 'left' }}>
            {m.posterPath ? (
              <img src={posterURL(m.posterPath)} alt={m.title} width={154} height={231} />
            ) : (
              <div style={{ width: 154, height: 231, background: '#222' }} />
            )}
            <div>{m.title} ({m.year})</div>
            <small>{m.status}</small>
          </button>
        ))}
      </div>
    </section>
  )
}

function AddMovie({ onDone }: { onDone: () => void }) {
  const [term, setTerm] = useState('')
  const [results, setResults] = useState<MovieCandidate[]>([])
  const [busy, setBusy] = useState(false)

  async function search() {
    setBusy(true)
    try {
      const r = await getJSON<{ items: MovieCandidate[] }>(`/movies/lookup?term=${encodeURIComponent(term)}`)
      setResults(r.items)
    } finally {
      setBusy(false)
    }
  }
  async function add(c: MovieCandidate) {
    await postJSON('/movies', { tmdbId: c.tmdbId })
    onDone()
  }

  return (
    <section>
      <button onClick={onDone}>&larr; Back</button>
      <h2>Add movie</h2>
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
