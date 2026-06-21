import { useEffect, useState, Fragment } from 'react'
import { getJSON } from '../api'
import { Icon, Loading, ErrorBanner, Empty, ago } from '../ui'

interface Download { id: number; name: string; state: string; mediaType: string; progress: number; protocol: string; createdAt: string }
interface BgTask { id: number; type: string; label: string; state: string; current?: number; total?: number; details?: string[]; error?: string; createdAt: string; finishedAt?: string }

const DL_PILL: Record<string, string> = {
  downloading: 'downloading', seeding: 'downloading', completed: 'available',
  queued: 'wanted', pending: 'wanted', submitting: 'searching', healing: 'searching',
}
const TASK_PILL: Record<string, string> = { running: 'searching', done: 'available', error: 'broken', queued: 'wanted' }

export function Activity() {
  const [data, setData] = useState<{ downloads: Download[]; tasks: BgTask[] } | null>(null)
  const [err, setErr] = useState('')
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  function load() {
    getJSON<{ downloads: Download[]; tasks: BgTask[] }>('/activity')
      .then((d) => { setData(d); setErr('') })
      .catch((e: unknown) => setErr(String(e)))
  }
  // Poll while the page is open so progress + task state stay live.
  useEffect(() => { load(); const t = setInterval(load, 3000); return () => clearInterval(t) }, [])

  if (err && !data) return <ErrorBanner message={err} />
  if (!data) return <Loading />

  const active = data.tasks.filter((t) => t.state === 'queued' || t.state === 'running')

  return (
    <section>
      <div className="season-head"><Icon name="download" /><h3>Download queue</h3>
        <span className="muted" style={{ marginLeft: 'auto', fontFamily: 'var(--mono)', fontSize: 12 }}>{data.downloads.length} active</span>
      </div>
      {data.downloads.length === 0 ? (
        <Empty icon="download" title="Nothing downloading" hint="Grabbed releases show here with live TorBox progress." />
      ) : (
        <div className="table-wrap">
          <table className="tbl">
            <thead><tr><th>Release</th><th style={{ width: 90 }}>Type</th><th style={{ width: 200 }}>Progress</th><th style={{ width: 130 }}>State</th></tr></thead>
            <tbody>
              {data.downloads.map((d) => (
                <tr key={d.id}>
                  <td className="rel-title">{d.name}</td>
                  <td className="muted">{d.mediaType}</td>
                  <td><Progress pct={d.progress} /></td>
                  <td><span className={`status ${DL_PILL[d.state] ?? 'idle'}`}>{d.state}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="season-head" style={{ marginTop: 26 }}><Icon name="refresh" /><h3>Background tasks</h3>
        <span className="muted" style={{ marginLeft: 'auto', fontFamily: 'var(--mono)', fontSize: 12 }}>{active.length} running</span>
      </div>
      {data.tasks.length === 0 ? (
        <Empty icon="refresh" title="No recent tasks" hint="Adopting or deleting WebDAV content runs here in the background." />
      ) : (
        <div className="table-wrap">
          <table className="tbl">
            <thead><tr><th style={{ width: 90 }}>Action</th><th>Item</th><th style={{ width: 130 }}>State</th><th style={{ width: 120 }}>When</th></tr></thead>
            <tbody>
              {data.tasks.map((t) => {
                const details = t.details ?? []
                const open = expanded.has(t.id)
                return (
                  <Fragment key={t.id}>
                    <tr className={details.length ? 'clickable' : ''}
                      onClick={() => details.length && setExpanded((s) => { const n = new Set(s); n.has(t.id) ? n.delete(t.id) : n.add(t.id); return n })}>
                      <td className="muted" style={{ textTransform: 'capitalize' }}>{t.type}</td>
                      <td className="rel-title">
                        {details.length > 0 && <span className="muted" style={{ marginRight: 6 }}>{open ? '▾' : '▸'}</span>}
                        {t.label}
                        {details.length > 0 && <span className="muted" style={{ fontSize: 11, marginLeft: 6 }}>({details.length})</span>}
                        {t.error && <div className="test-bad" style={{ fontSize: 11, marginTop: 3 }}>{t.error}</div>}
                      </td>
                      <td>
                        <span className={`status ${TASK_PILL[t.state] ?? 'idle'}`}>{t.state}</span>
                        {t.state === 'running' && (t.total ?? 0) > 0 && (
                          <span className="muted" style={{ fontSize: 11, marginLeft: 8 }}>{t.current}/{t.total}</span>
                        )}
                      </td>
                      <td className="muted" style={{ fontSize: 12 }}>{ago(t.finishedAt || t.createdAt)}</td>
                    </tr>
                    {open && (
                      <tr className="task-details-row">
                        <td />
                        <td colSpan={3}>
                          <ul className="task-details">{details.map((d, i) => <li key={i}>{d}</li>)}</ul>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function Progress({ pct }: { pct: number }) {
  const v = Math.max(0, Math.min(100, pct || 0))
  return (
    <div className="progress" title={`${v}%`}>
      <div className="progress-fill" style={{ width: `${v}%` }} />
      <span className="progress-label">{v}%</span>
    </div>
  )
}
