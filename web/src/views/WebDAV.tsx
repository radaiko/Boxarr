import { useEffect, useState, type MouseEvent } from 'react'
import { getJSON, postJSON, posterURL } from '../api'
import { Icon, Empty, Loading, ErrorBanner, gb } from '../ui'
import { AdoptPicker } from './AdoptPicker'

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
  posterPath?: string
}

// Cover shows the catalog poster for tracked items, or a placeholder for unknown.
function Cover({ poster }: { poster?: string }) {
  return poster
    ? <img className="wd-cover" src={posterURL(poster)} alt="" loading="lazy" />
    : <span className="wd-cover wd-cover-unknown"><Icon name="film" /></span>
}

// Sel is the multiselect handle threaded to rows/groups.
interface Sel {
  has: (id: number) => boolean
  toggle: (id: number) => void
  toggleMany: (ids: number[], on: boolean) => void
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
  const [selected, setSelected] = useState<Set<number>>(new Set())

  function reload(): Promise<void> {
    return getJSON<{ items: Item[] }>('/webdav')
      .then((r) => { setItems(r.items); setErr(''); setSelected(new Set()) })
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(() => { void reload() }, [])

  async function refresh() {
    setBusy(true)
    try { await postJSON('/webdav/refresh', {}); await reload() }
    catch (e) { setErr(String(e)) }
    finally { setBusy(false) }
  }

  const sel: Sel = {
    has: (id) => selected.has(id),
    toggle: (id) => setSelected((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n }),
    toggleMany: (ids, on) => setSelected((s) => { const n = new Set(s); ids.forEach((id) => on ? n.add(id) : n.delete(id)); return n }),
  }

  if (items === null && err) return <ErrorBanner message={err} />
  if (!items) return <Loading />

  const total = items.reduce((s, it) => s + it.size, 0)
  const selItems = items.filter((it) => selected.has(it.id))
  const trackedSel = selItems.filter((it) => it.known).length
  const allIds = items.map((it) => it.id)
  const allSelected = items.length > 0 && selected.size === items.length

  return (
    <section>
      {err && <ErrorBanner message={err} />}
      <div className="row-between" style={{ marginBottom: 14 }}>
        <label className="wd-selectall">
          <input type="checkbox" checked={allSelected}
            onChange={(e) => sel.toggleMany(allIds, e.target.checked)} />
          <span className="muted">{items.length} {items.length === 1 ? 'item' : 'items'} · {gb(total)} on the mount</span>
        </label>
        <button className="btn" onClick={() => void refresh()} disabled={busy}>
          <Icon name="refresh" /> {busy ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>

      {selected.size > 0 && (
        <BulkBar count={selected.size} tracked={trackedSel} size={selItems.reduce((s, it) => s + it.size, 0)}
          onDelete={() => postJSON('/webdav/delete', { ids: [...selected] }).then(() => reload())}
          onClear={() => setSelected(new Set())} />
      )}

      {items.length === 0 ? (
        <Empty icon="webdav" title="Mount is empty"
          hint="Releases Boxarr pulls through TorBox appear here, grouped by title. Refresh scans the mount now." />
      ) : (
        SECTIONS.map((s) => {
          const inKind = items.filter((it) => it.kind === s.kind)
          if (inKind.length === 0) return null
          return <Section key={s.kind} label={s.label} icon={s.icon} kind={s.kind} items={inKind}
            grouped={s.kind === 'series' || s.kind === 'anime'} reload={reload} sel={sel} />
        })
      )}
    </section>
  )
}

// BulkBar acts on the current multiselect: delete all selected (with an inline
// confirm that warns when tracked items are included).
function BulkBar({ count, tracked, size, onDelete, onClear }: {
  count: number; tracked: number; size: number; onDelete: () => Promise<void>; onClear: () => void
}) {
  const [armed, setArmed] = useState(false)
  const [busy, setBusy] = useState(false)
  return (
    <div className="bulk-bar">
      <span className="bulk-count">{count} selected · {gb(size)}</span>
      {tracked > 0 && <span className="test-bad" style={{ fontSize: 12 }}>{tracked} tracked (removed from library)</span>}
      <span style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
        {!armed ? (
          <button className="btn btn-danger btn-sm" onClick={() => setArmed(true)}><Icon name="trash" /> Delete selected</button>
        ) : (
          <>
            <button className="btn btn-danger btn-sm" disabled={busy}
              onClick={async () => { setBusy(true); try { await onDelete() } finally { setBusy(false) } }}>
              <Icon name="check" /> {busy ? 'Deleting…' : `Confirm delete ${count}`}
            </button>
            <button className="btn btn-sm" disabled={busy} onClick={() => setArmed(false)}>Cancel</button>
          </>
        )}
        <button className="btn btn-ghost btn-sm" onClick={onClear}>Clear</button>
      </span>
    </div>
  )
}

function Section({ label, icon, items, grouped, kind, reload, sel }: {
  label: string; icon: string; items: Item[]; grouped: boolean; kind: string; reload: () => Promise<void>; sel: Sel
}) {
  const size = items.reduce((s, it) => s + it.size, 0)
  const adoptKind = kind === 'unknown' ? '' : kind // unknown → let the server auto-detect
  const groups = grouped ? groupByTitle(items) : []
  return (
    <div style={{ marginBottom: 24 }}>
      <div className="season-head">
        <Icon name={icon} />
        <h3>{label}</h3>
        <span className="muted" style={{ marginLeft: 'auto', fontFamily: 'var(--mono)', fontSize: 12 }}>
          {grouped ? `${groups.length} titles · ` : ''}{items.length} files · {gb(size)}
        </span>
      </div>
      {grouped
        ? groups.map((g) => <TitleGroup key={g.title} group={g} kind={adoptKind} reload={reload} sel={sel} />)
        : <FlatTable items={items} adoptKind={adoptKind} reload={reload} sel={sel} />}
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

function TitleGroup({ group, kind, reload, sel }: { group: Group; kind: string; reload: () => Promise<void>; sel: Sel }) {
  const unknown = group.items.filter((i) => !i.known)
  const trackedCount = group.items.length - unknown.length
  const ids = group.items.map((i) => i.id)
  const allSel = ids.every((id) => sel.has(id))
  // Status lives on the show, not per episode: tracked (all imported), unknown
  // (none), or partial (some).
  const status = trackedCount === group.items.length ? 'tracked' : trackedCount === 0 ? 'unknown' : 'partial'
  return (
    <details className="wd-group">
      <summary>
        <input type="checkbox" className="wd-check" checked={allSel}
          onClick={stop} onChange={(e) => sel.toggleMany(ids, e.target.checked)} />
        <Cover poster={group.items.find((i) => i.posterPath)?.posterPath} />
        <span className="wd-group-title">{group.title}</span>
        <span className="wd-group-right" onClick={stop}>
          <span className={`status ${status === 'tracked' ? 'available' : status === 'partial' ? 'searching' : 'idle'}`}>
            {status === 'partial' ? `${trackedCount}/${group.items.length} tracked` : status}
          </span>
          <span className="wd-group-meta">{group.items.length} files · {gb(group.size)}</span>
          {unknown.length > 0 && <AdoptBtn items={unknown} kind={kind} reload={reload} label="Add show to library" />}
          <DeleteBtn ids={ids} reload={reload} tracked={trackedCount} title="Delete show from TorBox" />
        </span>
      </summary>
      <div className="table-wrap" style={{ margin: '8px 0 4px' }}>
        <table className="tbl">
          <tbody>
            {group.items.map((it) => (
              <tr key={it.id} className={sel.has(it.id) ? 'row-sel' : ''}>
                <td style={{ width: 34 }}><input type="checkbox" className="wd-check" checked={sel.has(it.id)} onChange={() => sel.toggle(it.id)} /></td>
                <td style={{ width: 70 }}>{it.season || it.episode ? <span className="ep-num">{epLabel(it)}</span> : ''}</td>
                <td className="rel-title">{it.name}</td>
                <td className="num" style={{ width: 90 }}>{gb(it.size)}</td>
                <td className="act-cell" style={{ width: 70 }}><DeleteBtn ids={[it.id]} reload={reload} tracked={it.known ? 1 : 0} /></td>
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

function FlatTable({ items, adoptKind, reload, sel }: { items: Item[]; adoptKind: string; reload: () => Promise<void>; sel: Sel }) {
  return (
    <div className="table-wrap">
      <table className="tbl">
        <thead><tr><th style={{ width: 34 }}></th><th style={{ width: 48 }}></th><th>Name</th><th className="right">Size</th><th>Source</th><th></th></tr></thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id} className={sel.has(it.id) ? 'row-sel' : ''}>
              <td><input type="checkbox" className="wd-check" checked={sel.has(it.id)} onChange={() => sel.toggle(it.id)} /></td>
              <td><Cover poster={it.posterPath} /></td>
              <td className="rel-title">{it.name}</td>
              <td className="num">{gb(it.size)}</td>
              <td>{it.known ? <span className="status available">tracked</span> : <span className="status idle">unknown</span>}</td>
              <td className="act-cell" style={{ width: 130 }}>
                {!it.known && <AdoptBtn items={[it]} kind={adoptKind} reload={reload} />}
                <DeleteBtn ids={[it.id]} reload={reload} tracked={it.known ? 1 : 0} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// AdoptBtn opens a search-and-pick dialog so the user chooses the exact TMDB
// match, then imports the item(s) into it.
function AdoptBtn({ items, kind, reload, label }: { items: Item[]; kind: string; reload: () => Promise<void>; label?: string }) {
  const [open, setOpen] = useState(false)
  const lbl = label ?? 'Add to library'
  const term = items[0]?.title || items[0]?.name || ''
  return (
    <span className="confirm-del" onClick={stop}>
      <button className="btn-icon" aria-label={lbl} title={lbl} onClick={(e) => { stop(e); setOpen(true) }}>
        <Icon name="plus" />
      </button>
      {open && (
        <AdoptPicker ids={items.map((i) => i.id)} defaultKind={kind || 'movie'} defaultTerm={term}
          onClose={() => setOpen(false)} onDone={reload} />
      )}
    </span>
  )
}

// DeleteBtn removes items from TorBox + the mount, with an inline confirm.
function DeleteBtn({ ids, reload, title, tracked = 0 }: {
  ids: number[]; reload: () => Promise<void>; title?: string; tracked?: number
}) {
  const [armed, setArmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const lbl = title ?? 'Delete from TorBox'
  if (busy) return <span className="muted" style={{ fontSize: 11 }}>deleting…</span>
  if (!armed) {
    return <button className="btn-icon" aria-label={lbl} title={lbl} onClick={(e) => { stop(e); setArmed(true) }}><Icon name="trash" /></button>
  }
  async function confirm(e: MouseEvent) {
    stop(e); setBusy(true); setErr('')
    try {
      const r = await postJSON<{ deleted: number; failed: number }>('/webdav/delete', { ids })
      if (r.failed > 0) setErr(`${r.failed} couldn’t be deleted`)
      await reload()
    } catch (ex) { setErr(shorten(String(ex))) } finally { setBusy(false) }
  }
  return (
    <span className="confirm-del" onClick={stop}>
      {tracked > 0 && <span className="test-bad" style={{ fontSize: 11 }}>removes {tracked} from library</span>}
      <button className="btn-icon danger" aria-label="Confirm delete" title="Confirm delete" onClick={confirm}><Icon name="check" /></button>
      <button className="btn-icon" aria-label="Cancel" title="Cancel" onClick={(e) => { stop(e); setArmed(false); setErr('') }}><Icon name="back" /></button>
      {err && <span className="test-bad" style={{ fontSize: 11 }}>{err}</span>}
    </span>
  )
}

function stop(e: MouseEvent) { e.preventDefault(); e.stopPropagation() }
function shorten(s: string): string { return s.length > 60 ? s.slice(0, 57) + '…' : s }
