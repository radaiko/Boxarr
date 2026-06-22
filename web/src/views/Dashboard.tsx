import { useEffect, useState } from 'react'
import { getJSON } from '../api'
import { Icon, Loading, gb, ago } from '../ui'

interface Counts { movies: number; series: number; anime: number; activeJobs: number; unreadNotifications: number }
interface StatusResp { version: string; counts: Counts }
interface StorageResp {
  usedBytes: number
  plan?: { tierName: string; concurrentSlots: number }
  downloads?: { active?: number; queued?: number }
  limits?: { dailyCap: number; usedToday: number; cooldownUntil: string }
}
interface Download { id: number; name: string; state: string; mediaType: string; progress: number; protocol: string }
interface HistoryItem { id: number; name: string; state: string; createdAt: string }
interface ActivityResp { downloads: Download[]; history: HistoryItem[] }
interface Note { id: number; type: string; read: boolean; createdAt: string; payload: Record<string, unknown> }

type Nav = (view: string) => void
type OpenCatalog = (kind: string, id: number) => void

// Dashboard is the at-a-glance landing view: library + TorBox stats, what needs
// attention, what's downloading now, and what was recently added.
export function Dashboard({ onNavigate, onOpenCatalog }: { onNavigate: Nav; onOpenCatalog: OpenCatalog }) {
  const [status, setStatus] = useState<StatusResp | null>(null)
  const [storage, setStorage] = useState<StorageResp | null>(null)
  const [activity, setActivity] = useState<ActivityResp | null>(null)
  const [notes, setNotes] = useState<Note[]>([])

  useEffect(() => {
    const load = () => {
      getJSON<StatusResp>('/status').then(setStatus).catch(() => {})
      getJSON<StorageResp>('/storage').then(setStorage).catch(() => {})
      getJSON<ActivityResp>('/activity').then(setActivity).catch(() => {})
      getJSON<{ items: Note[] }>('/notifications').then((r) => setNotes(r.items)).catch(() => {})
    }
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])

  if (!status) return <Loading />
  const c = status.counts
  const queue = activity?.downloads ?? []
  const downloading = queue.filter((d) => d.state === 'downloading')
  const attention = notes.filter((n) => !n.read)

  return (
    <section className="dash">
      <div className="stat-grid">
        <StatCard label="Library" value={`${c.movies + c.series + c.anime}`}
          sub={`${c.movies} movies · ${c.series} series · ${c.anime} anime`} onClick={() => onNavigate('Movies')} />
        <StatCard label="On TorBox" value={storage ? gb(storage.usedBytes) : '—'}
          sub={storage?.plan ? storage.plan.tierName : 'connect TorBox'} onClick={() => onNavigate('TorBox')} />
        <StatCard label="Downloading" value={`${downloading.length}`}
          sub={`${queue.length - downloading.length} queued${storage?.plan ? ` · ${storage.plan.concurrentSlots} slots` : ''}`}
          onClick={() => onNavigate('Activity')} />
        <StatCard label="Grabs today" value={`${storage?.limits?.usedToday ?? 0}`}
          sub={storage?.limits && storage.limits.dailyCap > 0 ? `cap ${storage.limits.dailyCap}` : 'no cap'}
          onClick={() => onNavigate('TorBox')} />
      </div>

      <div className="dash-cols">
        <Panel title="Needs attention" icon="notifications" count={attention.length} onMore={() => onNavigate('Notifications')}>
          {attention.length === 0 ? (
            <p className="dash-empty">All clear — nothing needs you.</p>
          ) : (
            attention.slice(0, 6).map((n) => {
              const id = Number(n.payload.catalogId)
              const kind = typeof n.payload.kind === 'string' ? n.payload.kind : ''
              const clickable = id > 0 && kind
              return (
                <div key={n.id} className={`dash-row${clickable ? ' clickable' : ''}`}
                  onClick={clickable ? () => onOpenCatalog(kind, id) : () => onNavigate('Notifications')}>
                  <span className={`status ${tone(n.type)}`}>{n.type.replace(/_/g, ' ')}</span>
                  <span className="dash-row-main">{summarize(n)}</span>
                  <span className="muted dash-row-time">{ago(n.createdAt)}</span>
                </div>
              )
            })
          )}
        </Panel>

        <Panel title="Downloading now" icon="download" count={downloading.length} onMore={() => onNavigate('Activity')}>
          {downloading.length === 0 ? (
            <p className="dash-empty">{queue.length > 0 ? `${queue.length} queued, none active yet.` : 'Nothing downloading.'}</p>
          ) : (
            downloading.slice(0, 6).map((d) => (
              <div key={d.id} className="dash-row">
                <span className="dash-row-main" title={d.name}>{d.name}</span>
                <span className="dash-pct">{d.progress}%</span>
              </div>
            ))
          )}
        </Panel>

        <Panel title="Recently added" icon="check" count={activity?.history?.length ?? 0} onMore={() => onNavigate('Activity')}>
          {!activity || activity.history.length === 0 ? (
            <p className="dash-empty">No imports yet.</p>
          ) : (
            activity.history.filter((h) => h.state === 'imported').slice(0, 6).map((h) => (
              <div key={h.id} className="dash-row">
                <span className="dash-row-main" title={h.name}>{h.name}</span>
                <span className="muted dash-row-time">{ago(h.createdAt)}</span>
              </div>
            ))
          )}
        </Panel>
      </div>
    </section>
  )
}

function StatCard({ label, value, sub, onClick }: { label: string; value: string; sub?: string; onClick?: () => void }) {
  return (
    <button className="stat dash-stat" onClick={onClick}>
      <div className="label">{label}</div>
      <div className="value">{value}</div>
      {sub && <div className="sub">{sub}</div>}
    </button>
  )
}

function Panel({ title, icon, count, onMore, children }: {
  title: string; icon: string; count: number; onMore: () => void; children: React.ReactNode
}) {
  return (
    <div className="dash-panel">
      <div className="season-head">
        <Icon name={icon} /><h3>{title}</h3>
        <span className="muted" style={{ fontFamily: 'var(--mono)', fontSize: 12 }}>{count}</span>
        <button className="btn btn-sm btn-ghost" style={{ marginLeft: 'auto' }} onClick={onMore}>View all</button>
      </div>
      <div className="dash-panel-body">{children}</div>
    </div>
  )
}

function tone(type: string): string {
  if (type.includes('fail') || type.includes('broken') || type.includes('error')) return 'broken'
  if (type === 'unknown_content' || type.includes('missing')) return 'wanted'
  if (type.includes('import') || type.includes('completed')) return 'available'
  return 'searching'
}

function summarize(n: Note): string {
  const p = n.payload || {}
  for (const key of ['title', 'name', 'item', 'message', 'error']) {
    if (typeof p[key] === 'string') return p[key] as string
  }
  return n.type.replace(/_/g, ' ')
}
