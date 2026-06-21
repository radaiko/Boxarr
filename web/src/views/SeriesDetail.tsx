import { useEffect, useState } from 'react'
import { getJSON, postJSON, putJSON, del, posterURL, type Series, type Episode, type Release, type ListResponse } from '../api'
import { Icon, Status, Loading, initials, MetaChips, ago } from '../ui'
import { SearchOverlay } from './SearchOverlay'

export function SeriesDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const [series, setSeries] = useState<Series | null>(null)
  const [releases, setReleases] = useState<Release[] | null>(null)
  const [scope, setScope] = useState('')
  const [scopeLabel, setScopeLabel] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchBusy, setSearchBusy] = useState(false)
  const [searchCurrent, setSearchCurrent] = useState<string | undefined>()
  const [msg, setMsg] = useState('')
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set())
  const toggleSeason = (n: number) =>
    setCollapsed((c) => { const x = new Set(c); x.has(n) ? x.delete(n) : x.add(n); return x })
  const [msgOk, setMsgOk] = useState(false)

  function reload() {
    getJSON<Series>(`/series/${id}`).then(setSeries).catch((e: unknown) => setMsg(String(e)))
  }
  useEffect(reload, [id])

  async function runSearch(path: string, sc: string, label: string, current?: string) {
    setScope(sc); setScopeLabel(label); setSearchCurrent(current)
    setSearchOpen(true); setSearchBusy(true); setReleases(null); setMsg('')
    try {
      const r = await getJSON<ListResponse<Release>>(path)
      setReleases(r.items)
    } catch (e) { setMsg(String(e)); setSearchOpen(false) } finally { setSearchBusy(false) }
  }
  const searchEpisode = (ep: Episode) =>
    runSearch(`/series/${id}/episodes/${ep.id}/search`, `episode:${ep.id}`, `S${pad(ep.seasonNumber)}E${pad(ep.episodeNumber)}`, ep.file?.name)
  const searchSeason = (n: number) =>
    runSearch(`/series/${id}/seasons/${n}/search`, `season:${n}`, `Season ${n}`)

  async function grab(rel: Release) {
    const [kind, ref] = scope.split(':')
    const body: Record<string, unknown> = { releaseId: rel.releaseId, scope: kind }
    if (kind === 'episode') body.episodeId = Number(ref)
    setSearchOpen(false)
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

      {searchOpen && (
        <SearchOverlay title={`${series.title} · ${scopeLabel}`} releases={releases ?? []}
          currentName={searchCurrent} busy={searchBusy} onGrab={(r) => void grab(r)} onClose={() => setSearchOpen(false)} />
      )}

      {series.seasons?.map((s) => (
        <div key={s.seasonNumber}>
          <div className="season-head season-toggle" onClick={() => toggleSeason(s.seasonNumber)}>
            <span className="muted" style={{ width: 14 }}>{collapsed.has(s.seasonNumber) ? '▸' : '▾'}</span>
            <h3>Season {s.seasonNumber}</h3>
            <Status value={s.status} />
            <span className="muted" style={{ fontSize: 12 }}>{s.episodes?.length ?? 0} eps</span>
            <button className="btn btn-sm" style={{ marginLeft: 'auto' }}
              onClick={(e) => { e.stopPropagation(); void searchSeason(s.seasonNumber) }}>
              <Icon name="search" /> Search season
            </button>
          </div>
          {!collapsed.has(s.seasonNumber) && (
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
                    <td className="muted" style={{ width: 120, fontSize: 11.5 }}>{ep.lastSearched ? `searched ${ago(ep.lastSearched)}` : ''}</td>
                    <td style={{ width: 130 }}><Status value={ep.status} hasFile={ep.hasFile} /></td>
                    <td className="right" style={{ width: 110 }}>
                      <button className="btn btn-sm btn-ghost" onClick={() => void searchEpisode(ep)}><Icon name="search" /> Search</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          )}
        </div>
      ))}
    </section>
  )
}

function pad(n: number): string { return String(n).padStart(2, '0') }
