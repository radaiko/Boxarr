import { useEffect, useState } from 'react'
import { getJSON, postJSON } from '../api'
import { Icon, Empty, Loading, ErrorBanner, gb } from '../ui'

interface Item {
  id: number
  name: string
  size: number
  category: string
  known: boolean
}

export function WebDAV() {
  const [items, setItems] = useState<Item[] | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  function reload() {
    getJSON<{ items: Item[] }>('/webdav').then((r) => setItems(r.items)).catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  async function refresh() {
    setBusy(true)
    try { await postJSON('/webdav/refresh', {}); reload() } finally { setBusy(false) }
  }

  if (err) return <ErrorBanner message={err} />
  if (!items) return <Loading />

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 18 }}>
        <span className="muted">{items.length} {items.length === 1 ? 'item' : 'items'} on the mount</span>
        <button className="btn" onClick={() => void refresh()} disabled={busy}>
          <Icon name="refresh" /> {busy ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      {items.length === 0 ? (
        <Empty icon="webdav" title="Mount is empty"
          hint="Releases Boxarr pulls through TorBox appear here. Refresh scans the WebDAV mount now." />
      ) : (
        <div className="table-wrap">
          <table className="tbl">
            <thead><tr><th>Name</th><th>Category</th><th className="right">Size</th><th>Source</th></tr></thead>
            <tbody>
              {items.map((it) => (
                <tr key={it.id}>
                  <td className="rel-title">{it.name}</td>
                  <td><span className="chip">{it.category}</span></td>
                  <td className="num">{gb(it.size)}</td>
                  <td>{it.known ? <span className="status available">tracked</span> : <span className="status wanted">unknown</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
