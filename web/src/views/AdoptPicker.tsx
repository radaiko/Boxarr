import { useEffect, useState } from 'react'
import { getJSON, postJSON, posterURL } from '../api'
import { Icon } from '../ui'

interface Cand { tmdbId: number; title: string; year: number; overview?: string; posterPath?: string }

const TYPES: { kind: string; label: string }[] = [
  { kind: 'movie', label: 'Movie' },
  { kind: 'series', label: 'Series' },
  { kind: 'anime', label: 'Anime' },
]

// AdoptPicker: search TMDB for the release and let the user pick the exact match,
// then import the given mount items into it. Used for both a single item and a
// whole show (all ids adopt into the one picked entry).
export function AdoptPicker({ ids, defaultKind, defaultTerm, onClose, onDone }: {
  ids: number[]
  defaultKind: string
  defaultTerm: string
  onClose: () => void
  onDone: () => Promise<void>
}) {
  const [kind, setKind] = useState(defaultKind || 'movie')
  const [term, setTerm] = useState(defaultTerm)
  const [results, setResults] = useState<Cand[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [adopting, setAdopting] = useState(false)
  const [err, setErr] = useState('')

  async function search() {
    if (!term.trim()) return
    setSearching(true); setErr(''); setResults(null)
    try {
      const path = kind === 'movie' ? 'movies' : 'series'
      const r = await getJSON<{ items: Cand[] }>(`/${path}/lookup?term=${encodeURIComponent(term)}`)
      setResults(r.items)
    } catch (e) { setErr(String(e)) } finally { setSearching(false) }
  }
  // Auto-search on open and whenever the type changes.
  useEffect(() => { void search() }, [kind]) // eslint-disable-line react-hooks/exhaustive-deps

  async function pick(c: Cand) {
    setAdopting(true); setErr('')
    try {
      for (const id of ids) await postJSON(`/webdav/${id}/adopt`, { kind, tmdbId: c.tmdbId })
      await onDone()
      onClose()
    } catch (e) { setErr(shorten(String(e))); setAdopting(false) }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Add to library">
        <div className="modal-head">
          <span className="modal-title">Add to library</span>
          <span className="muted" style={{ fontSize: 12 }}>{ids.length > 1 ? `${ids.length} files` : ''}</span>
          <button className="btn-icon" aria-label="Close" title="Close" style={{ marginLeft: 'auto' }} onClick={onClose}><Icon name="back" /></button>
        </div>
        <div className="modal-body">
          <div className="row" style={{ display: 'flex', gap: 10, marginBottom: 12 }}>
            <div className="seg" role="tablist" aria-label="Library type">
              {TYPES.map((t) => (
                <button key={t.kind} role="tab" aria-selected={kind === t.kind}
                  className={kind === t.kind ? 'active' : ''} onClick={() => setKind(t.kind)}>{t.label}</button>
              ))}
            </div>
            <input className="input" style={{ flex: 1 }} value={term} placeholder="Search title…"
              onChange={(e) => setTerm(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void search() }} />
            <button className="btn" onClick={() => void search()} disabled={searching}>
              <Icon name="search" /> {searching ? 'Searching…' : 'Search'}
            </button>
          </div>

          {err && <div className="test-bad" style={{ marginBottom: 10 }}>{err}</div>}
          {adopting && <div className="muted" style={{ marginBottom: 10 }}>Adding to library…</div>}

          {results === null ? (
            !err && <div className="muted">Searching…</div>
          ) : results.length === 0 ? (
            <div className="muted">No matches — refine the title or switch the type.</div>
          ) : (
            results.map((c) => (
              <button key={c.tmdbId} className="pick-row" onClick={() => void pick(c)} disabled={adopting}>
                {c.posterPath
                  ? <img className="pick-poster" src={posterURL(c.posterPath)} alt="" />
                  : <span className="pick-poster" />}
                <span className="pick-meta">
                  <span className="pick-title">{c.title} {c.year ? <span className="pick-year">({c.year})</span> : null}</span>
                  {c.overview && <span className="pick-overview">{c.overview}</span>}
                </span>
                <Icon name="plus" />
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

function shorten(s: string): string { return s.length > 100 ? s.slice(0, 97) + '…' : s }
