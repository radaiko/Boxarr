import { useEffect, useState } from 'react'
import { getJSON, postJSON, putJSON, del, posterURL, type Series, type Episode, type Release, type ListResponse } from '../api'
import { Icon, Status, Loading, initials, MetaChips } from '../ui'
import { ReleaseTable } from './ReleaseTable'

export function SeriesDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [series, setSeries] = useState<Series | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [scope, setScope] = useState('')
  const [scopeLabel, setScopeLabel] = useState('')
  const [msg, setMsg] = useState('')
  const [msgOk, setMsgOk] = useState(false)

  function reload() {
    getJSON<Series>(`/series/${id}`).then(setSeries).catch((e: unknown) => setMsg(String(e)))
  }
  useEffect(reload, [id])

  async function runSearch(path: string, sc: string, label: string) {
    setMsg('Searching…'); setMsgOk(false); setScope(sc); setScopeLabel(label); setReleases(null)
    try {
      const r = await getJSON<ListResponse<Release>>(path)
      setReleases(r.items); setMsg(r.items.length ? '' : 'No releases found.')
    } catch (e) { setMsg(String(e)) }
  }
  const searchEpisode = (ep: Episode) =>
    runSearch(`/series/${id}/episodes/${ep.id}/search`, `episode:${ep.id}`, `S${pad(ep.seasonNumber)}E${pad(ep.episodeNumber)}`)
  const searchSeason = (n: number) =>
    runSearch(`/series/${id}/seasons/${n}/search`, `season:${n}`, `Season ${n}`)

  async function grab(rel: Release) {
    const [kind, ref] = scope.split(':')
    const body: Record<string, unknown> = { releaseId: rel.releaseId, scope: kind }
    if (kind === 'episode') body.episodeId = Number(ref)
    setMsg(`Grabbing ${rel.title}…`); setMsgOk(false)
    await postJSON(`/series/${id}/grab`, body)
    setMsg('Grabbed — download queued on TorBox.'); setMsgOk(true); setReleases(null); setScope('')
  }
  async function remove() { await del(`/series/${id}`); onBack() }
  async function convertType() {
    const to = series?.seriesType === 'anime' ? 'standard' : 'anime'
    setMsg(`Moving to ${to === 'anime' ? 'anime' : 'series'} library…`); setMsgOk(false)
    try { await putJSON(`/series/${id}/type`, { seriesType: to }); reload(); setMsg(`Moved to ${to === 'anime' ? 'anime' : 'series'}.`); setMsgOk(true) }
    catch (e) { setMsg(String(e)) }
  }

  if (!series) return <Loading />

  return (
    <section>
      <button className="btn btn-ghost btn-sm" onClick={onBack} style={{ marginBottom: 16 }}><Icon name="back" /> Library</button>
      <div className="detail-head">
        <div className="detail-poster">
          {series.posterPath ? <img src={posterURL(series.posterPath)} alt="" />
            : <div className="poster-fallback">{initials(series.title)}</div>}
        </div>
        <div className="detail-body">
          <h2>{series.title}</h2>
          <div className="detail-meta">
            <span>{series.year || '—'}</span>
            <Status value={series.status} />
          </div>
          {series.overview && <p className="overview">{series.overview}</p>}
          <div className="topbar-actions" style={{ justifyContent: 'flex-start' }}>
            <button className="btn" onClick={() => void convertType()}>
              <Icon name="anime" /> {series.seriesType === 'anime' ? 'Move to series' : 'Move to anime'}
            </button>
            <button className="btn btn-danger" onClick={() => void remove()}><Icon name="trash" /> Delete series</button>
          </div>
          {msg && <p className={`toast${msgOk ? ' ok' : ''}`} style={{ marginTop: 12 }}>{msg}</p>}
        </div>
      </div>

      {releases && (
        <div className="panel" style={{ marginTop: 22 }}>
          <div className="row-between" style={{ marginBottom: 12 }}>
            <strong>Releases · {scopeLabel}</strong>
            <button className="btn btn-ghost btn-sm" onClick={() => { setReleases(null); setScope('') }}>Close</button>
          </div>
          <ReleaseTable releases={releases} onGrab={(r) => void grab(r)} />
        </div>
      )}

      {series.seasons?.map((s) => (
        <div key={s.seasonNumber}>
          <div className="season-head">
            <h3>Season {s.seasonNumber}</h3>
            <Status value={s.status} />
            <button className="btn btn-sm" style={{ marginLeft: 'auto' }} onClick={() => void searchSeason(s.seasonNumber)}>
              <Icon name="search" /> Search season
            </button>
          </div>
          <div className="table-wrap">
            <table className="tbl">
              <tbody>
                {s.episodes?.map((ep) => (
                  <tr key={ep.id}>
                    <td style={{ width: 56 }}><span className="ep-num">E{pad(ep.episodeNumber)}</span></td>
                    <td>
                      {ep.title || '—'}
                      {ep.file && (
                        <div className="ep-file">
                          <span className="ep-file-name" title={ep.file.path || ep.file.name}>{ep.file.name}</span>
                          <MetaChips file={ep.file} />
                        </div>
                      )}
                    </td>
                    <td className="num" style={{ width: 110 }}>{ep.airDate || ''}</td>
                    <td style={{ width: 130 }}><Status value={ep.status} hasFile={ep.hasFile} /></td>
                    <td className="right" style={{ width: 110 }}>
                      <button className="btn btn-sm btn-ghost" onClick={() => void searchEpisode(ep)}><Icon name="search" /> Search</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ))}
    </section>
  )
}

function pad(n: number): string { return String(n).padStart(2, '0') }
