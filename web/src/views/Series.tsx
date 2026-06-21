import { useEffect, useState } from 'react'
import { getJSON, postJSON, posterURL, type Series as SeriesT, type SeriesCandidate, type ListResponse } from '../api'
import { Icon, Status, Empty, Loading, ErrorBanner, initials } from '../ui'
import { SeriesDetail } from './SeriesDetail'

export function Series({ anime = false, openId, openSeq }: { anime?: boolean; openId?: number; openSeq?: number }) {
  const [series, setSeries] = useState<SeriesT[] | null>(null)
  const [selected, setSelected] = useState<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState('')

  const noun = anime ? 'anime' : 'series'
  const Noun = anime ? 'anime' : 'series'

  function reload() {
    getJSON<ListResponse<SeriesT>>('/series')
      .then((r) => setSeries(r.items.filter((s) => (s.seriesType === 'anime') === anime)))
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [anime])
  // Open a specific series when jumped to (e.g. a TorBox tracked link).
  useEffect(() => { if (openId != null) setSelected(openId) }, [openSeq]) // eslint-disable-line react-hooks/exhaustive-deps

  if (err) return <ErrorBanner message={err} />
  if (selected !== null) return <SeriesDetail id={selected} onBack={() => { setSelected(null); reload() }} />
  if (adding) return <AddSeries anime={anime} onDone={() => { setAdding(false); reload() }} />
  if (!series) return <Loading />

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 18 }}>
        <span className="muted">{series.length} {Noun}</span>
        <button className="btn btn-primary" onClick={() => setAdding(true)}><Icon name="plus" /> Add {noun}</button>
      </div>
      {series.length === 0 ? (
        <Empty icon={anime ? 'anime' : 'series'} title={`No ${noun} yet`}
          hint={anime
            ? 'Add an anime — Boxarr tracks seasons and files it in your anime library.'
            : 'Add a show — Boxarr tracks each season and grabs episodes as they air.'}
          action={<button className="btn btn-primary" onClick={() => setAdding(true)}><Icon name="plus" /> Add {noun}</button>} />
      ) : (
        <div className="poster-grid">
          {series.map((s) => (
            <button key={s.id} className="poster-card" onClick={() => setSelected(s.id)}>
              <div className="poster">
                {s.posterPath
                  ? <img src={posterURL(s.posterPath)} alt={s.title} loading="lazy" />
                  : <div className="poster-fallback">{initials(s.title)}</div>}
              </div>
              <div className="poster-title">{s.title}</div>
              <div className="row-between">
                <span className="poster-meta">{s.year || '—'}</span>
                <Status value={s.status} />
              </div>
            </button>
          ))}
        </div>
      )}
    </section>
  )
}

function AddSeries({ anime, onDone }: { anime: boolean; onDone: () => void }) {
  const [term, setTerm] = useState('')
  const [results, setResults] = useState<SeriesCandidate[] | null>(null)
  const [busy, setBusy] = useState(false)

  async function search() {
    if (!term) return
    setBusy(true)
    try {
      const r = await getJSON<{ items: SeriesCandidate[] }>(`/series/lookup?term=${encodeURIComponent(term)}`)
      setResults(r.items)
    } finally {
      setBusy(false)
    }
  }
  async function add(c: SeriesCandidate) {
    await postJSON('/series', { tmdbId: c.tmdbId, seriesType: anime ? 'anime' : 'standard' })
    onDone()
  }

  return (
    <section>
      <button className="btn btn-ghost btn-sm" onClick={onDone} style={{ marginBottom: 14 }}><Icon name="back" /> Back</button>
      <div className="panel">
        <div className="search">
          <input className="input" autoFocus value={term} placeholder="Search TMDB for a show…"
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
