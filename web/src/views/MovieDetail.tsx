import { useEffect, useState } from 'react'
import { getJSON, postJSON, del, posterURL, type Movie, type Release, type ListResponse } from '../api'
import { Icon, Status, Loading, initials, FilePanel } from '../ui'
import { toast } from '../toast'
import { SearchOverlay } from './SearchOverlay'

export function MovieDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [movie, setMovie] = useState<Movie | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [searchOpen, setSearchOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [msgOk, setMsgOk] = useState(false)

  function reload() {
    getJSON<Movie>(`/movies/${id}`).then(setMovie).catch((e: unknown) => setMsg(String(e)))
  }
  useEffect(reload, [id])

  async function search() {
    setSearchOpen(true); setBusy(true); setMsg(''); setReleases(null)
    try {
      const r = await getJSON<ListResponse<Release>>(`/movies/${id}/search`)
      setReleases(r.items)
    } catch (e) { setMsg(String(e)); setMsgOk(false); setSearchOpen(false) } finally { setBusy(false) }
  }
  async function reset() {
    setBusy(true)
    try {
      await postJSON(`/movies/${id}/reset`, {})
      toast('Reset — re-searching for a working release.', 'ok')
      reload()
    } catch (e) {
      toast('Reset failed: ' + String(e), 'err')
    } finally {
      setBusy(false)
    }
  }

  async function grab(rel: Release) {
    setSearchOpen(false)
    setMsg(`Grabbing ${rel.title}…`); setMsgOk(false)
    try {
      await postJSON(`/movies/${id}/grab`, { releaseId: rel.releaseId })
      setMsg('Grabbed — download queued on TorBox.'); setMsgOk(true); setReleases(null)
      toast('Grabbed — queued on TorBox.', 'ok')
      reload()
    } catch (e) {
      setMsg('Grab failed: ' + String(e)); setMsgOk(false); toast('Grab failed: ' + String(e), 'err')
    }
  }
  async function remove() {
    await del(`/movies/${id}`); onBack()
  }

  if (!movie) return <Loading />

  return (
    <section>
      <button className="btn btn-ghost btn-sm" onClick={onBack} style={{ marginBottom: 16 }}><Icon name="back" /> Library</button>
      <div className="detail-head">
        <div className="detail-poster">
          {movie.posterPath ? <img src={posterURL(movie.posterPath)} alt="" />
            : <div className="poster-fallback">{initials(movie.title)}</div>}
        </div>
        <div className="detail-body">
          <h2>{movie.title}</h2>
          <div className="detail-meta">
            <span>{movie.year || '—'}</span>
            <Status value={movie.status} hasFile={movie.hasFile} />
          </div>
          {movie.overview && <p className="overview">{movie.overview}</p>}
          {movie.status === 'failed' && (
            <div className="fail-box">
              <span><b>Download failed.</b> {movie.lastError || 'The grab failed on TorBox.'}</span>
              <button className="btn btn-sm" onClick={() => void reset()} disabled={busy}>
                <Icon name="refresh" /> Retry
              </button>
            </div>
          )}
          {movie.file && <FilePanel file={movie.file} />}
          <div className="topbar-actions" style={{ justifyContent: 'flex-start' }}>
            <button className="btn btn-primary" onClick={() => void search()} disabled={busy}>
              <Icon name="search" /> {busy ? 'Searching…' : 'Search releases'}
            </button>
            <button className="btn btn-danger" onClick={() => void remove()}><Icon name="trash" /> Delete</button>
          </div>
          {msg && <p className={`toast${msgOk ? ' ok' : ''}`} style={{ marginTop: 12 }}>{msg}</p>}
        </div>
      </div>

      {searchOpen && (
        <SearchOverlay title={`${movie.title}${movie.year ? ` (${movie.year})` : ''}`} releases={releases ?? []}
          currentName={movie.file?.name} busy={busy} onGrab={(r) => void grab(r)} onClose={() => setSearchOpen(false)} />
      )}
    </section>
  )
}
