import { useEffect, useState } from 'react'
import { getJSON, postJSON, posterURL, type Movie, type MovieCandidate, type ListResponse } from '../api'
import { Icon, Status, Empty, Loading, ErrorBanner, initials } from '../ui'
import { MovieDetail } from './MovieDetail'

export function Movies({ openId, openSeq }: { openId?: number; openSeq?: number } = {}) {
  const [movies, setMovies] = useState<Movie[] | null>(null)
  const [selected, setSelected] = useState<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState('')

  function reload() {
    getJSON<ListResponse<Movie>>('/movies')
      .then((r) => setMovies(r.items))
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])
  // Open a specific movie when jumped to (e.g. a TorBox tracked link).
  useEffect(() => { if (openId != null) setSelected(openId) }, [openSeq]) // eslint-disable-line react-hooks/exhaustive-deps

  if (err) return <ErrorBanner message={err} />
  if (selected !== null) return <MovieDetail id={selected} onBack={() => { setSelected(null); reload() }} />
  if (adding) return <AddMovie onDone={() => { setAdding(false); reload() }} />
  if (!movies) return <Loading />

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 18 }}>
        <span className="muted">{movies.length} {movies.length === 1 ? 'movie' : 'movies'}</span>
        <button className="btn btn-primary" onClick={() => setAdding(true)}><Icon name="plus" /> Add movie</button>
      </div>
      {movies.length === 0 ? (
        <Empty icon="film" title="No movies yet"
          hint="Add a movie and Boxarr will find a release and pull it through TorBox."
          action={<button className="btn btn-primary" onClick={() => setAdding(true)}><Icon name="plus" /> Add movie</button>} />
      ) : (
        <div className="poster-grid">
          {movies.map((m) => (
            <button key={m.id} className="poster-card" onClick={() => setSelected(m.id)}>
              <div className="poster">
                {m.posterPath
                  ? <img src={posterURL(m.posterPath)} alt={m.title} loading="lazy" />
                  : <div className="poster-fallback">{initials(m.title)}</div>}
              </div>
              <div className="poster-title">{m.title}</div>
              <div className="row-between">
                <span className="poster-meta">{m.year || '—'}</span>
                <Status value={m.status} hasFile={m.hasFile} />
              </div>
            </button>
          ))}
        </div>
      )}
    </section>
  )
}

function AddMovie({ onDone }: { onDone: () => void }) {
  const [term, setTerm] = useState('')
  const [results, setResults] = useState<MovieCandidate[] | null>(null)
  const [busy, setBusy] = useState(false)

  async function search() {
    if (!term) return
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
      <button className="btn btn-ghost btn-sm" onClick={onDone} style={{ marginBottom: 14 }}><Icon name="back" /> Back</button>
      <div className="panel">
        <div className="search">
          <input className="input" autoFocus value={term} placeholder="Search TMDB for a movie…"
            onChange={(e) => setTerm(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && void search()} />
          <button className="btn btn-primary" onClick={() => void search()} disabled={busy || !term}>
            <Icon name="search" /> Search
          </button>
        </div>
      </div>
      {busy && <Loading />}
      {results && !busy && (
        <div className="poster-grid" style={{ marginTop: 16 }}>
          {results.map((c) => (
            <div key={c.tmdbId} className="poster-card" style={{ cursor: 'default' }}>
              <div className="poster">
                {c.posterPath ? <img src={posterURL(c.posterPath)} alt={c.title} loading="lazy" />
                  : <div className="poster-fallback">{initials(c.title)}</div>}
              </div>
              <div className="poster-title">{c.title}</div>
              <div className="row-between">
                <span className="poster-meta">{c.year || '—'}</span>
                {c.inLibrary
                  ? <span className="chip">in library</span>
                  : <button className="btn btn-sm btn-primary" onClick={() => void add(c)}><Icon name="plus" /> Add</button>}
              </div>
            </div>
          ))}
          {results.length === 0 && <Empty icon="search" title="No matches" hint="Try a different title." />}
        </div>
      )}
    </section>
  )
}
