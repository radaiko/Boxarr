import { useEffect, useState } from 'react'
import { getJSON, putJSON, postJSON } from '../api'

interface Note {
  id: number
  type: string
  read: boolean
  createdAt: string
  payload: Record<string, unknown>
}

export function Notifications() {
  const [notes, setNotes] = useState<Note[]>([])
  const [err, setErr] = useState('')

  function reload() {
    getJSON<{ items: Note[] }>('/notifications')
      .then((r) => setNotes(r.items))
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  async function markRead(id: number) {
    await putJSON(`/notifications/${id}/read`, {})
    reload()
  }
  async function markAll() {
    await putJSON('/notifications/read-all', {})
    reload()
  }
  async function act(id: number, action: string) {
    await postJSON(`/notifications/${id}/action`, { action })
    reload()
  }

  if (err) return <p>Error: {err}</p>

  return (
    <section>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2>Notifications</h2>
        <button onClick={() => void markAll()}>Mark all read</button>
      </div>
      {notes.length === 0 && <p>No notifications.</p>}
      <ul>
        {notes.map((n) => (
          <li key={n.id} style={{ fontWeight: n.read ? 'normal' : 'bold' }}>
            <code>{n.type}</code> — {summarize(n)} <small>{n.createdAt}</small>{' '}
            {n.type === 'unknown_content' && !n.read && (
              <>
                <button onClick={() => void act(n.id, 'ignore')}>Keep</button>{' '}
                <button onClick={() => void act(n.id, 'delete')}>Delete from TorBox</button>{' '}
              </>
            )}
            {!n.read && <button onClick={() => void markRead(n.id)}>✓</button>}
          </li>
        ))}
      </ul>
    </section>
  )
}

function summarize(n: Note): string {
  const p = n.payload || {}
  const parts: string[] = []
  for (const key of ['title', 'name', 'error', 'limit']) {
    if (typeof p[key] === 'string') parts.push(p[key] as string)
  }
  return parts.join(' · ')
}
