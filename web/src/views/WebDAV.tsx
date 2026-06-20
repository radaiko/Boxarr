import { useEffect, useState } from 'react'
import { getJSON } from '../api'

interface Item {
  id: number
  name: string
  size: number
  category: string
  known: boolean
}

export function WebDAV() {
  const [items, setItems] = useState<Item[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    getJSON<{ items: Item[] }>('/webdav')
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setErr(String(e)))
  }, [])

  if (err) return <p>Error: {err}</p>

  return (
    <section>
      <h2>WebDAV mount ({items.length})</h2>
      <table>
        <thead>
          <tr><th>Name</th><th>Category</th><th>Size</th><th>Known</th></tr>
        </thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id}>
              <td>{it.name}</td>
              <td>{it.category}</td>
              <td>{(it.size / 1e9).toFixed(2)} GB</td>
              <td>{it.known ? 'yes' : 'unknown'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
