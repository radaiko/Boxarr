import { useEffect, useState } from 'react'
import { getJSON, postJSON } from '../api'
import { Icon, Loading, gb, ago } from '../ui'
import { toast } from '../toast'

interface Counts {
  movies: number; series: number; anime: number; activeJobs: number; unreadNotifications: number
  missingMovies?: number; missingEpisodes?: number
}
interface StatusResp { version: string; counts: Counts }
interface StorageResp {
  usedBytes: number
  plan?: { tierName: string; concurrentSlots: number }
  downloads?: { active?: number; queued?: number }
  limits?: { dailyCap: number; usedToday: number; cooldownUntil: string }
}
interface Download { id: number; name: string; state: string; mediaType: string; progress: number; protocol: string }
interface HistoryItem { id: number; name: string; state: string; createdAt: string }
interface BgTask { id: number; type: string; label: string; state: string; current?: number; total?: number }
interface ScheduleItem { name: string; everySeconds: number; lastRun: string; nextRun: string }
interface ActivityResp { downloads: Download[]; history: HistoryItem[]; tasks?: BgTask[]; schedule?: ScheduleItem[] }
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
  const missing = (c.missingMovies ?? 0) + (c.missingEpisodes ?? 0)
  const runningTasks = (activity?.tasks ?? []).filter((t) => t.state === 'running')
  const schedule = activity?.schedule ?? []

  async function run(path: string, what: string) {
    try { await postJSON(path, {}); toast(`${what} started — see Activity.`, 'ok') }
    catch (e) { toast(`${what} failed: ${String(e)}`, 'err') }
  }

  return (
    <section className="dash">
      <div className="dash-actions">
        <button className="btn btn-sm btn-primary" onClick={() => void run('/search/missing', 'Search all missing')}>
          <Icon name="search" /> Search all missing
        </button>
        <button className="btn btn-sm" onClick={() => void run('/library/refresh', 'Library refresh')}>
          <Icon name="refresh" /> Refresh from TorBox + Plex
        </button>
        <button className="btn btn-sm" onClick={() => void run('/upgrade/search', 'Upgrade search')}>
          <Icon name="refresh" /> Search upgrades
        </button>
      </div>
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
        <StatCard label="Missing" value={`${missing}`}
          sub={`${c.missingMovies ?? 0} movies · ${c.missingEpisodes ?? 0} episodes`}
          onClick={() => onNavigate('Movies')} />
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

      <div className="dash-cols">
        <Panel title="Running now" icon="refresh" count={runningTasks.length + downloading.length} onMore={() => onNavigate('Activity')}>
          {runningTasks.length === 0 && downloading.length === 0 ? (
            <p className="dash-empty">Idle — no background work running.</p>
          ) : (
            <>
              {runningTasks.map((t) => (
                <div key={`t${t.id}`} className="dash-row">
                  <span className="status searching" style={{ textTransform: 'capitalize' }}>{t.type}</span>
                  <span className="dash-row-main" title={t.label}>{t.label}</span>
                  {(t.total ?? 0) > 0 && <span className="dash-pct">{t.current}/{t.total}</span>}
                </div>
              ))}
              {downloading.length > 0 && (
                <div className="dash-row">
                  <span className="status downloading">downloads</span>
                  <span className="dash-row-main">{downloading.length} downloading on TorBox</span>
                </div>
              )}
            </>
          )}
        </Panel>

        <Panel title="Up next" icon="refresh" count={schedule.length} onMore={() => onNavigate('Activity')}>
          {schedule.length === 0 ? (
            <p className="dash-empty">No scheduled tasks yet.</p>
          ) : (
            schedule.slice(0, 8).map((s) => (
              <div key={s.name} className="dash-row">
                <span className="dash-row-main">{s.name}</span>
                <span className="muted dash-row-time" title={`every ${fmtEvery(s.everySeconds)}`}>{until(s.nextRun)}</span>
              </div>
            ))
          )}
        </Panel>
      </div>
    </section>
  )
}

// until formats a future ISO timestamp as "in 5m" / "due now".
function until(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (ms <= 0) return 'due now'
  const m = Math.round(ms / 60000)
  if (m < 60) return `in ${m}m`
  const h = Math.round(m / 60)
  if (h < 24) return `in ${h}h`
  return `in ${Math.round(h / 24)}d`
}

function fmtEvery(sec: number): string {
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  if (sec < 86400) return `${Math.round(sec / 3600)}h`
  return `${Math.round(sec / 86400)}d`
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
