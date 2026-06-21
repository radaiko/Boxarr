import { useEffect, useState, Fragment } from 'react'
import { getJSON, type FileMeta } from '../api'
import { Icon, Loading, ErrorBanner, Empty, ago, gb, MetaChips } from '../ui'

interface Download {
  id: number; name: string; state: string; mediaType: string; progress: number; protocol: string
  createdAt: string; size?: number; downloaded?: number; etaSeconds?: number; release?: FileMeta
}
interface HistoryItem {
  id: number; name: string; state: string; mediaType: string; size?: number; protocol: string
  createdAt: string; error?: string; release?: FileMeta
}
interface BgTask {
  id: number; type: string; label: string; state: string; current?: number; total?: number
  details?: string[]; error?: string; createdAt: string; finishedAt?: string
}
interface ActivityResp { downloads: Download[]; tasks: BgTask[]; history: HistoryItem[] }

const DL_PILL: Record<string, string> = {
  downloading: 'downloading', seeding: 'downloading', completed: 'available',
  queued: 'queued', pending: 'queued', submitting: 'queued', healing: 'searching',
  imported: 'available', failed: 'broken', heal_failed: 'broken', manually_resolved: 'idle',
}
const TASK_PILL: Record<string, string> = { running: 'searching', done: 'available', error: 'broken', queued: 'wanted' }
const TABS = ['Queue', 'History', 'Tasks'] as const
type Tab = typeof TABS[number]

export function Activity() {
  const [data, setData] = useState<ActivityResp | null>(null)
  const [err, setErr] = useState('')
  const [tab, setTab] = useState<Tab>('Queue')
  const [count, setCount] = useState(50)
  const [q, setQ] = useState('')
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  function load() {
    getJSON<ActivityResp>('/activity').then((d) => { setData(d); setErr('') }).catch((e: unknown) => setErr(String(e)))
  }
  useEffect(() => { load(); const t = setInterval(load, 3000); return () => clearInterval(t) }, [])

  if (err && !data) return <ErrorBanner message={err} />
  if (!data) return <Loading />

  const ql = q.trim().toLowerCase()
  const hit = (s: string) => !ql || s.toLowerCase().includes(ql)
  const dls = data.downloads.filter((d) => hit(d.name))
  const hist = data.history.filter((d) => hit(d.name))
  const tsk = data.tasks.filter((t) => hit(t.label) || (t.details ?? []).some(hit))
  const counts: Record<Tab, number> = { Queue: dls.length, History: hist.length, Tasks: tsk.length }

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 16, gap: 12, flexWrap: 'wrap' }}>
        <div className="tabs" style={{ margin: 0 }}>
          {TABS.map((t) => (
            <button key={t} className={`tab${tab === t ? ' active' : ''}`} onClick={() => setTab(t)}>
              {t}<span className="muted" style={{ marginLeft: 6, fontSize: 11 }}>{counts[t]}</span>
            </button>
          ))}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginLeft: 'auto' }}>
          <div className="search-box">
            <Icon name="search" />
            <input className="input" type="search" placeholder="Filter by name…" value={q}
              onChange={(e) => setQ(e.target.value)} />
          </div>
          <label className="chk">Show
            <select className="input" style={{ width: 'auto', marginLeft: 8 }} value={count} onChange={(e) => setCount(Number(e.target.value))}>
              {[25, 50, 100].map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          </label>
        </div>
      </div>

      {tab === 'Queue' && <QueueTable rows={dls.slice(0, count)} />}
      {tab === 'History' && <HistoryTable rows={hist.slice(0, count)} />}
      {tab === 'Tasks' && <TasksTable rows={tsk.slice(0, count)} expanded={expanded} setExpanded={setExpanded} />}
    </section>
  )
}

function ReleaseCell({ name, protocol, size, release, error }: {
  name: string; protocol: string; size?: number; release?: FileMeta; error?: string
}) {
  return (
    <td className="rel-title">
      {name}
      <div className="dl-meta">
        <span className="chip">{protocol}</span>
        {(size ?? 0) > 0 && <span className="muted">{gb(size!)}</span>}
        {release && <MetaChips file={release} />}
      </div>
      {error && <div className="test-bad" style={{ fontSize: 11, marginTop: 3 }}>{error}</div>}
    </td>
  )
}

function QueueTable({ rows }: { rows: Download[] }) {
  if (rows.length === 0) return <Empty icon="download" title="Nothing downloading" hint="Grabbed releases show here with live TorBox progress." />
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead><tr><th>Release</th><th style={{ width: 90 }}>Type</th><th style={{ width: 210 }}>Progress</th><th style={{ width: 130 }}>State</th></tr></thead>
        <tbody>
          {rows.map((d) => (
            <tr key={d.id}>
              <ReleaseCell name={d.name} protocol={d.protocol} size={d.size} release={d.release} />
              <td className="muted">{d.mediaType}</td>
              <td>
                <Progress pct={d.progress} />
                {(d.etaSeconds ?? 0) > 0 && d.state === 'downloading' && (
                  <div className="muted" style={{ fontSize: 11, marginTop: 3 }}>~{eta(d.etaSeconds!)} left</div>
                )}
              </td>
              <td><span className={`status ${DL_PILL[d.state] ?? 'idle'}`}>{d.state}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function HistoryTable({ rows }: { rows: HistoryItem[] }) {
  if (rows.length === 0) return <Empty icon="download" title="No download history" hint="Finished and failed downloads are kept here." />
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead><tr><th>Release</th><th style={{ width: 90 }}>Type</th><th style={{ width: 130 }}>State</th><th style={{ width: 120 }}>When</th></tr></thead>
        <tbody>
          {rows.map((d) => (
            <tr key={d.id}>
              <ReleaseCell name={d.name} protocol={d.protocol} size={d.size} release={d.release} error={d.error} />
              <td className="muted">{d.mediaType}</td>
              <td><span className={`status ${DL_PILL[d.state] ?? 'idle'}`}>{d.state}</span></td>
              <td className="muted" style={{ fontSize: 12 }}>{ago(d.createdAt)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function TasksTable({ rows, expanded, setExpanded }: {
  rows: BgTask[]; expanded: Set<number>; setExpanded: (f: (s: Set<number>) => Set<number>) => void
}) {
  if (rows.length === 0) return <Empty icon="refresh" title="No tasks" hint="Adopting or deleting WebDAV content runs here in the background." />
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead><tr><th style={{ width: 90 }}>Action</th><th>Item</th><th style={{ width: 130 }}>State</th><th style={{ width: 120 }}>When</th></tr></thead>
        <tbody>
          {rows.map((t) => {
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
                    <td colSpan={3}><ul className="task-details">{details.map((d, i) => <li key={i}>{d}</li>)}</ul></td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function eta(s: number): string {
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  return `${Math.floor(s / 3600)}h ${Math.round((s % 3600) / 60)}m`
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
