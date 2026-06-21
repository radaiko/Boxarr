import { useEffect, useState } from 'react'
import { getJSON, putJSON, postJSON, del } from '../api'
import { Icon, Empty, Loading, ErrorBanner, ago } from '../ui'

interface Note {
  id: number
  type: string
  read: boolean
  createdAt: string
  payload: Record<string, unknown>
}

export function Notifications() {
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [err, setErr] = useState('')

  function reload() {
    getJSON<{ items: Note[] }>('/notifications').then((r) => setNotes(r.items)).catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  const markRead = async (id: number) => { await putJSON(`/notifications/${id}/read`, {}); reload() }
  const markAll = async () => { await putJSON('/notifications/read-all', {}); reload() }
  const clearAll = async () => { await del('/notifications'); reload() }
  const act = async (id: number, action: string) => { await postJSON(`/notifications/${id}/action`, { action }); reload() }

  if (err) return <ErrorBanner message={err} />
  if (!notes) return <Loading />

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 18 }}>
        <span className="muted">{notes.filter((n) => !n.read).length} unread</span>
        {notes.length > 0 && (
          <span style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={() => void markAll()}><Icon name="check" /> Mark all read</button>
            <button className="btn btn-danger" onClick={() => void clearAll()}><Icon name="trash" /> Clear all</button>
          </span>
        )}
      </div>
      {notes.length === 0 ? (
        <Empty icon="notifications" title="All clear" hint="Grabs, imports, failures, and unknown content on the mount show up here." />
      ) : (
        <div className="note-list">
          {notes.map((n) => (
            <div key={n.id} className={`note${n.read ? '' : ' unread'}`}>
              <span className={`status ${noteTone(n.type)}`}>{n.type.replace(/_/g, ' ')}</span>
              <span className="note-summary">{summarize(n)}</span>
              <span className="note-time">{ago(n.createdAt)}</span>
              <span className="note-actions">
                {n.type === 'unknown_content' && !n.read && (
                  <>
                    <button className="btn btn-sm btn-primary" onClick={() => void act(n.id, 'adopt')}>Adopt to library</button>
                    <button className="btn btn-sm" onClick={() => void act(n.id, 'ignore')}>Keep</button>
                    <button className="btn btn-sm btn-danger" onClick={() => void act(n.id, 'delete')}>Delete from TorBox</button>
                  </>
                )}
                {!n.read && <button className="btn btn-sm btn-ghost" onClick={() => void markRead(n.id)}><Icon name="check" /></button>}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function noteTone(type: string): string {
  if (type.includes('fail') || type.includes('broken') || type.includes('error')) return 'broken'
  if (type.includes('completed') || type.includes('import')) return 'available'
  if (type === 'unknown_content') return 'wanted'
  return 'searching'
}

function summarize(n: Note): string {
  const p = n.payload || {}
  const parts: string[] = []
  for (const key of ['title', 'name', 'error', 'limit']) {
    if (typeof p[key] === 'string') parts.push(p[key] as string)
  }
  return parts.join(' · ') || '—'
}
