import { useEffect, useState } from 'react'
import { getJSON, postJSON, del, posterURL, type Movie, type Release, type ListResponse } from '../api'

export function MovieDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [movie, setMovie] = useState<Movie | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    getJSON<Movie>(`/movies/${id}`).then(setMovie).catch((e: unknown) => setMsg(String(e)))
  }, [id])

  async function search() {
    setBusy(true)
    setMsg('')
    try {
      const r = await getJSON<ListResponse<Release>>(`/movies/${id}/search`)
      setReleases(r.items)
    } catch (e) {
      setMsg(String(e))
    } finally {
      setBusy(false)
    }
  }
  async function grab(rel: Release) {
    setMsg(`Grabbing ${rel.title}…`)
    await postJSON(`/movies/${id}/grab`, { releaseId: rel.releaseId })
    setMsg('Grabbed — download queued.')
  }
  async function remove() {
    await del(`/movies/${id}`)
    onBack()
  }

  if (!movie) return <p>{msg || 'Loading…'}</p>

  return (
    <section>
      <button onClick={onBack}>&larr; Back</button>
      <h2>{movie.title} ({movie.year})</h2>
      <div style={{ display: 'flex', gap: 16 }}>
        {movie.posterPath && <img src={posterURL(movie.posterPath)} alt="" width={154} height={231} />}
        <div>
          <p>Status: <strong>{movie.status}</strong> {movie.hasFile ? '· has file' : ''}</p>
          {movie.overview && <p>{movie.overview}</p>}
          <button onClick={() => void search()} disabled={busy}>Search releases</button>{' '}
          <button onClick={() => void remove()}>Delete</button>
          {msg && <p>{msg}</p>}
        </div>
      </div>
      {releases && (
        <table>
          <thead>
            <tr><th>Title</th><th>Proto</th><th>Size</th><th>Quality</th><th>Health</th><th>Cached</th><th>Score</th><th /></tr>
          </thead>
          <tbody>
            {releases.map((r) => (
              <tr key={r.releaseId} style={{ opacity: r.rejected ? 0.5 : 1 }}>
                <td>{r.title}</td>
                <td>{r.protocol}</td>
                <td>{(r.size / 1e9).toFixed(2)} GB</td>
                <td>{r.quality}</td>
                <td>{r.seeders ?? r.grabs ?? '-'}</td>
                <td>{r.cached === null ? '-' : r.cached ? 'instant' : 'download'}</td>
                <td>{r.rejected ? 'rejected' : r.score}</td>
                <td><button onClick={() => void grab(r)} disabled={r.rejected}>Grab</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
