import { useEffect, useState } from 'react'
import { getJSON, postJSON } from '../api'
import { Icon, Empty, Loading, ErrorBanner, gb } from '../ui'

interface Item {
  id: number
  name: string
  size: number
  category: string
  known: boolean
  kind: string // movie | series | anime | unknown
  title: string
  season?: number
  episode?: number
}

const SECTIONS: { kind: string; label: string; icon: string }[] = [
  { kind: 'movie', label: 'Movies', icon: 'movies' },
  { kind: 'series', label: 'Series', icon: 'series' },
  { kind: 'anime', label: 'Anime', icon: 'anime' },
  { kind: 'unknown', label: 'Unknown', icon: 'webdav' },
]

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

  const total = items.reduce((s, it) => s + it.size, 0)

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 18 }}>
        <span className="muted">{items.length} {items.length === 1 ? 'item' : 'items'} · {gb(total)} on the mount</span>
        <button className="btn" onClick={() => void refresh()} disabled={busy}>
          <Icon name="refresh" /> {busy ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      {items.length === 0 ? (
        <Empty icon="webdav" title="Mount is empty"
          hint="Releases Boxarr pulls through TorBox appear here, grouped by title. Refresh scans the mount now." />
      ) : (
        SECTIONS.map((s) => {
          const inKind = items.filter((it) => it.kind === s.kind)
          if (inKind.length === 0) return null
          return <Section key={s.kind} label={s.label} icon={s.icon} items={inKind} grouped={s.kind === 'series' || s.kind === 'anime'} />
        })
      )}
    </section>
  )
}

function Section({ label, icon, items, grouped }: { label: string; icon: string; items: Item[]; grouped: boolean }) {
  const size = items.reduce((s, it) => s + it.size, 0)
  return (
    <div style={{ marginBottom: 24 }}>
      <div className="season-head">
        <Icon name={icon} />
        <h3>{label}</h3>
        <span className="muted" style={{ marginLeft: 'auto', fontFamily: 'var(--mono)', fontSize: 12 }}>
          {grouped ? `${groupByTitle(items).length} titles · ` : ''}{items.length} files · {gb(size)}
        </span>
      </div>
      {grouped ? groupByTitle(items).map((g) => <TitleGroup key={g.title} group={g} />) : <FlatTable items={items} />}
    </div>
  )
}

interface Group { title: string; items: Item[]; size: number }

function groupByTitle(items: Item[]): Group[] {
  const map = new Map<string, Item[]>()
  for (const it of items) {
    const key = it.title || it.name
    map.set(key, [...(map.get(key) ?? []), it])
  }
  return Array.from(map.entries())
    .map(([title, its]) => ({ title, items: its.sort(byEp), size: its.reduce((s, i) => s + i.size, 0) }))
    .sort((a, b) => a.title.localeCompare(b.title))
}

function byEp(a: Item, b: Item): number {
  return (a.season ?? 0) - (b.season ?? 0) || (a.episode ?? 0) - (b.episode ?? 0) || a.name.localeCompare(b.name)
}

function TitleGroup({ group }: { group: Group }) {
  return (
    <details className="wd-group">
      <summary>
        <span className="wd-group-title">{group.title}</span>
        <span className="wd-group-meta">{group.items.length} files · {gb(group.size)}</span>
      </summary>
      <div className="table-wrap" style={{ margin: '8px 0 4px' }}>
        <table className="tbl">
          <tbody>
            {group.items.map((it) => (
              <tr key={it.id}>
                <td style={{ width: 70 }}>{it.season || it.episode ? <span className="ep-num">{epLabel(it)}</span> : ''}</td>
                <td className="rel-title">{it.name}</td>
                <td className="num" style={{ width: 90 }}>{gb(it.size)}</td>
                <td style={{ width: 90 }}>{it.known ? <span className="chip">tracked</span> : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  )
}

function epLabel(it: Item): string {
  if (it.season && it.episode) return `S${pad(it.season)}E${pad(it.episode)}`
  if (it.episode) return `E${pad(it.episode)}`
  if (it.season) return `S${pad(it.season)}`
  return ''
}
function pad(n: number): string { return String(n).padStart(2, '0') }

function FlatTable({ items }: { items: Item[] }) {
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead><tr><th>Name</th><th className="right">Size</th><th>Source</th></tr></thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id}>
              <td className="rel-title">{it.name}</td>
              <td className="num">{gb(it.size)}</td>
              <td>{it.known ? <span className="status available">tracked</span> : <span className="status idle">unknown</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
