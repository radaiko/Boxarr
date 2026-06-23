import { useEffect, useRef, useState } from 'react'
import { getJSON } from '../api'
import { Icon } from '../ui'

interface LogEntry { time: string; level: string; msg: string; attrs?: Record<string, string> }

const LEVELS = ['', 'INFO', 'WARN', 'ERROR'] as const

// Logs is a live tail of the server's recent log records (in-memory ring buffer)
// so you can see what Boxarr is doing without shelling into the container.
export function Logs() {
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [level, setLevel] = useState('')
  const [q, setQ] = useState('')
  const [live, setLive] = useState(true)
  const [err, setErr] = useState('')
  const qRef = useRef(q)
  const levelRef = useRef(level)
  qRef.current = q
  levelRef.current = level

  useEffect(() => {
    let stop = false
    const load = async () => {
      try {
        const params = new URLSearchParams({ limit: '500' })
        if (levelRef.current) params.set('level', levelRef.current)
        if (qRef.current.trim()) params.set('q', qRef.current.trim())
        const r = await getJSON<{ entries: LogEntry[] }>('/logs?' + params.toString())
        if (!stop) { setEntries(r.entries ?? []); setErr('') }
      } catch (e: unknown) {
        if (!stop) setErr(String(e))
      }
    }
    load()
    if (!live) return () => { stop = true }
    const id = setInterval(load, 3000)
    return () => { stop = true; clearInterval(id) }
  }, [live, level, q])

  return (
    <section className="logs">
      <div className="row-between" style={{ marginBottom: 12, gap: 10, flexWrap: 'wrap' }}>
        <div className="seg">
          {LEVELS.map((l) => (
            <button key={l || 'all'} className={`seg-btn${level === l ? ' on' : ''}`} onClick={() => setLevel(l)}>
              {l || 'All'}
            </button>
          ))}
        </div>
        <div className="search-box" style={{ flex: 1, minWidth: 200 }}>
          <Icon name="search" />
          <input className="input" type="search" placeholder="Filter messages + fields…" value={q}
            onChange={(e) => setQ(e.target.value)} />
        </div>
        <button className={`btn btn-sm${live ? ' btn-primary' : ''}`} onClick={() => setLive((v) => !v)}>
          <Icon name="refresh" /> {live ? 'Live' : 'Paused'}
        </button>
      </div>

      {err && <p className="dash-empty">Couldn't load logs: {err}</p>}
      {!err && entries.length === 0 && <p className="dash-empty">No log records match.</p>}

      <div className="log-list">
        {entries.map((e, i) => (
          <div key={i} className={`log-row lvl-${e.level.toLowerCase()}`}>
            <span className="log-time">{fmtTime(e.time)}</span>
            <span className={`log-lvl lvl-${e.level.toLowerCase()}`}>{e.level}</span>
            <span className="log-msg">
              {e.msg}
              {e.attrs && Object.keys(e.attrs).length > 0 && (
                <span className="log-attrs">
                  {Object.entries(e.attrs).map(([k, v]) => (
                    <span key={k} className="log-attr"><b>{k}</b>={v}</span>
                  ))}
                </span>
              )}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}

function fmtTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
