import { type Release } from '../api'
import { Icon, gb } from '../ui'

// ReleaseTable renders ranked Prowlarr releases for the grab picker, with
// language + subtitle info. Rejected releases (failed a hard filter) are dimmed.
// currentName highlights the release that's already grabbed for this item.
export function ReleaseTable({ releases, onGrab, currentName }: {
  releases: Release[]; onGrab: (r: Release) => void; currentName?: string
}) {
  if (releases.length === 0) {
    return <p className="muted" style={{ padding: '12px 2px' }}>No releases match.</p>
  }
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Release</th><th>Proto</th><th className="right">Size</th><th>Quality</th><th>Languages</th>
            <th className="right">Health</th><th>Cached</th><th className="right">Score</th><th className="grab-col" />
          </tr>
        </thead>
        <tbody>
          {releases.map((r) => (
            <tr key={r.releaseId} className={r.rejected ? 'rejected' : ''}>
              <td className="rel-title">
                {r.title}
                {currentName && r.title === currentName && <span className="chip" style={{ marginLeft: 8 }}>current</span>}
              </td>
              <td><span className="chip">{r.protocol}</span></td>
              <td className="num">{gb(r.size)}</td>
              <td>{r.resolution ? `${r.resolution} ${r.quality ?? ''}`.trim() : (r.quality || '—')}</td>
              <td>
                <span className="lang-cell">
                  {(r.languages ?? []).map((l) => <span key={l} className="meta-chip lang">{l}</span>)}
                  {r.subs && <span className="meta-chip" title="English subtitles detected">SUB</span>}
                  {!(r.languages?.length) && !r.subs && <span className="muted">—</span>}
                </span>
              </td>
              <td className="num">{r.seeders ?? r.grabs ?? '—'}</td>
              <td>{r.cached == null ? '—' : r.cached ? <span className="chip instant">instant</span> : <span className="muted">download</span>}</td>
              <td className="num">{r.rejected ? '—' : r.score}</td>
              <td className="right grab-col">
                <button className="btn btn-sm btn-primary" onClick={() => onGrab(r)} disabled={r.rejected}>
                  <Icon name="download" /> Grab
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
