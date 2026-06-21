import { type Release } from '../api'
import { Icon, gb } from '../ui'

// ReleaseTable renders ranked Prowlarr releases for the grab picker. Rejected
// releases (failed a hard filter) are dimmed and can't be grabbed.
export function ReleaseTable({ releases, onGrab }: { releases: Release[]; onGrab: (r: Release) => void }) {
  if (releases.length === 0) {
    return <p className="muted" style={{ padding: '12px 2px' }}>No releases found.</p>
  }
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Release</th><th>Proto</th><th className="right">Size</th><th>Quality</th>
            <th className="right">Health</th><th>Cached</th><th className="right">Score</th><th />
          </tr>
        </thead>
        <tbody>
          {releases.map((r) => (
            <tr key={r.releaseId} className={r.rejected ? 'rejected' : ''}>
              <td className="rel-title">{r.title}</td>
              <td><span className="chip">{r.protocol}</span></td>
              <td className="num">{gb(r.size)}</td>
              <td>{r.quality || '—'}</td>
              <td className="num">{r.seeders ?? r.grabs ?? '—'}</td>
              <td>{r.cached == null ? '—' : r.cached ? <span className="chip instant">instant</span> : <span className="muted">download</span>}</td>
              <td className="num">{r.rejected ? '—' : r.score}</td>
              <td className="right">
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
