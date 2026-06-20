import { useEffect, useState } from 'react'
import {
  getJSON, postJSON, del, posterURL,
  type Series, type Episode, type Release, type ListResponse,
} from '../api'

export function SeriesDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [series, setSeries] = useState<Series | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [scope, setScope] = useState('')
  const [msg, setMsg] = useState('')

  function reload() {
    getJSON<Series>(`/series/${id}`).then(setSeries).catch((e: unknown) => setMsg(String(e)))
  }
  useEffect(reload, [id])

  async function searchEpisode(ep: Episode) {
    setMsg('Searching…')
    setScope(`episode:${ep.id}`)
    try {
      const r = await getJSON<ListResponse<Release>>(`/series/${id}/episodes/${ep.id}/search`)
      setReleases(r.items)
      setMsg('')
    } catch (e) {
      setMsg(String(e))
    }
  }
  async function searchSeason(seasonNumber: number) {
    setMsg('Searching…')
    setScope(`season:${seasonNumber}`)
    try {
      const r = await getJSON<ListResponse<Release>>(`/series/${id}/seasons/${seasonNumber}/search`)
      setReleases(r.items)
      setMsg('')
    } catch (e) {
      setMsg(String(e))
    }
  }
  async function grab(rel: Release) {
    const [kind, ref] = scope.split(':')
    const body: Record<string, unknown> = { releaseId: rel.releaseId, scope: kind }
    if (kind === 'episode') body.episodeId = Number(ref)
    setMsg(`Grabbing ${rel.title}…`)
    await postJSON(`/series/${id}/grab`, body)
    setMsg('Grabbed — download queued.')
    setReleases(null)
  }
  async function remove() {
    await del(`/series/${id}`)
    onBack()
  }

  if (!series) return <p>{msg || 'Loading…'}</p>

  return (
    <section>
      <button onClick={onBack}>&larr; Back</button>
      <h2>{series.title} ({series.year})</h2>
      <div style={{ display: 'flex', gap: 16 }}>
        {series.posterPath && <img src={posterURL(series.posterPath)} alt="" width={154} height={231} />}
        <div>
          <p>Status: <strong>{series.status}</strong></p>
          {series.overview && <p>{series.overview}</p>}
          <button onClick={() => void remove()}>Delete series</button>
          {msg && <p>{msg}</p>}
        </div>
      </div>

      {series.seasons?.map((s) => (
        <div key={s.seasonNumber}>
          <h3>
            Season {s.seasonNumber} — {s.status}{' '}
            <button onClick={() => void searchSeason(s.seasonNumber)}>Search season</button>
          </h3>
          <table>
            <tbody>
              {s.episodes?.map((ep) => (
                <tr key={ep.id}>
                  <td>E{String(ep.episodeNumber).padStart(2, '0')}</td>
                  <td>{ep.title}</td>
                  <td>{ep.airDate}</td>
                  <td>{ep.hasFile ? 'available' : ep.status}</td>
                  <td><button onClick={() => void searchEpisode(ep)}>Search</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}

      {releases && (
        <div>
          <h3>Releases ({scope})</h3>
          <table>
            <thead>
              <tr><th>Title</th><th>Proto</th><th>Size</th><th>Health</th><th>Cached</th><th>Score</th><th /></tr>
            </thead>
            <tbody>
              {releases.map((r) => (
                <tr key={r.releaseId} style={{ opacity: r.rejected ? 0.5 : 1 }}>
                  <td>{r.title}</td>
                  <td>{r.protocol}</td>
                  <td>{(r.size / 1e9).toFixed(2)} GB</td>
                  <td>{r.seeders ?? r.grabs ?? '-'}</td>
                  <td>{r.cached === null ? '-' : r.cached ? 'instant' : 'download'}</td>
                  <td>{r.rejected ? 'rejected' : r.score}</td>
                  <td><button onClick={() => void grab(r)} disabled={r.rejected}>Grab</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
