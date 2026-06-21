import { useEffect, useState } from 'react'
import { getJSON, postJSON, del, posterURL, type Movie, type Release, type ListResponse } from '../api'
import { Icon, Status, Loading, initials, FilePanel } from '../ui'
import { ReleaseTable } from './ReleaseTable'

export function MovieDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [movie, setMovie] = useState<Movie | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [msgOk, setMsgOk] = useState(false)

  useEffect(() => {
    getJSON<Movie>(`/movies/${id}`).then(setMovie).catch((e: unknown) => setMsg(String(e)))
  }, [id])

  async function search() {
    setBusy(true); setMsg(''); setReleases(null)
    try {
      const r = await getJSON<ListResponse<Release>>(`/movies/${id}/search`)
      setReleases(r.items)
      if (r.items.length === 0) { setMsg('No releases found.'); setMsgOk(false) }
    } catch (e) { setMsg(String(e)); setMsgOk(false) } finally { setBusy(false) }
  }
  async function grab(rel: Release) {
    setMsg(`Grabbing ${rel.title}…`); setMsgOk(false)
    await postJSON(`/movies/${id}/grab`, { releaseId: rel.releaseId })
    setMsg('Grabbed — download queued on TorBox.'); setMsgOk(true); setReleases(null)
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

      {busy && <Loading />}
      {releases && releases.length > 0 && (
        <div style={{ marginTop: 22 }}><ReleaseTable releases={releases} onGrab={(r) => void grab(r)} /></div>
      )}
    </section>
  )
}
