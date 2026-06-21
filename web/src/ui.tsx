// Shared UI primitives: inline-SVG icons, the lifecycle status pill (the app's
// signature), empty/loading/error states, and small formatters.
import type { ReactNode } from 'react'
import type { FileMeta } from './api'

const PATHS: Record<string, ReactNode> = {
  movies: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M3 9h18M8 4v5M16 4v5M8 20v-6M16 20v-6" /></>,
  series: <><rect x="3" y="5" width="18" height="12" rx="2" /><path d="M8 21h8M12 17v4" /></>,
  webdav: <><path d="M7 18a4 4 0 0 1 0-8 5 5 0 0 1 9.6-1.3A3.5 3.5 0 0 1 18 18H7z" /></>,
  storage: <><rect x="3" y="4" width="18" height="7" rx="2" /><rect x="3" y="13" width="18" height="7" rx="2" /><path d="M7 7.5h.01M7 16.5h.01" /></>,
  notifications: <><path d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6" /><path d="M10 20a2 2 0 0 0 4 0" /></>,
  settings: <><path d="M5 7h10M5 12h14M9 17h10" /><circle cx="17" cy="7" r="2" /><circle cx="6" cy="12" r="2" /><circle cx="13" cy="17" r="2" /></>,
  search: <><circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" /></>,
  plus: <><path d="M12 5v14M5 12h14" /></>,
  back: <><path d="M19 12H5M12 19l-7-7 7-7" /></>,
  check: <><path d="M20 6 9 17l-5-5" /></>,
  trash: <><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></>,
  download: <><path d="M12 3v12M7 10l5 5 5-5M5 21h14" /></>,
  refresh: <><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5" /></>,
  film: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M3 9h18M8 4v16M16 4v16" /></>,
  copy: <><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V5a2 2 0 0 1 2-2h10" /></>,
  anime: <><path d="M12 3.5l1.9 4.4 4.8.4-3.6 3.1 1.1 4.7L12 13.6 7.8 16.1l1.1-4.7L5.3 8.3l4.8-.4z" /><path d="M18.5 15.5l.7 1.6 1.8.2-1.4 1.1.4 1.7-1.5-.9-1.5.9.4-1.7-1.4-1.1 1.8-.2z" /></>,
  key: <><circle cx="7.5" cy="15.5" r="4.5" /><path d="m10.5 12.5 9-9M17 6l3 3M14 9l2 2" /></>,
}

export function Icon({ name }: { name: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {PATHS[name] ?? null}
    </svg>
  )
}

// Status renders the media-lifecycle state as a colored pill. hasFile always
// reads as available regardless of the stored status.
export function Status({ value, hasFile }: { value?: string; hasFile?: boolean }) {
  const v = hasFile ? 'available' : (value || 'missing')
  return <span className={`status ${v}`}>{v.replace('_', ' ')}</span>
}

export function Empty({ icon, title, hint, action }: { icon: string; title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="empty">
      <Icon name={icon} />
      <div className="empty-title">{title}</div>
      {hint && <div className="empty-hint">{hint}</div>}
      {action && <div style={{ marginTop: 10 }}>{action}</div>}
    </div>
  )
}

export function Loading() {
  return <div className="loading" role="status"><div className="spinner" /><span className="sr-only">Loading…</span></div>
}

export function ErrorBanner({ message }: { message: string }) {
  return <div className="banner-error">Couldn’t load this — {message}</div>
}

export function gb(bytes: number): string {
  if (!bytes) return '0 GB'
  if (bytes < 1e9) return (bytes / 1e6).toFixed(0) + ' MB'
  return (bytes / 1e9).toFixed(2) + ' GB'
}

export function ago(iso: string): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const s = Math.floor((Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

export function initials(title: string): string {
  return title.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase()
}

// MetaChips renders the parsed file attributes as small chips (resolution,
// dynamic range, source, codec, audio, languages, group).
export function MetaChips({ file }: { file: FileMeta }) {
  const chips: { label: string; cls?: string }[] = []
  if (file.resolution) chips.push({ label: file.resolution })
  if (file.dynamicRange) chips.push({ label: file.dynamicRange, cls: 'hdr' })
  const src = file.source || file.quality // parser puts BluRay/WEB-DL in quality
  if (src) chips.push({ label: src })
  if (file.codec) chips.push({ label: file.codec })
  if (file.audio) chips.push({ label: file.audio })
  for (const l of file.languages ?? []) chips.push({ label: l, cls: 'lang' })
  if (file.group) chips.push({ label: file.group, cls: 'grp' })
  if (chips.length === 0) return null
  return (
    <span className="meta-chips">
      {chips.map((c, i) => <span key={i} className={`meta-chip${c.cls ? ' ' + c.cls : ''}`}>{c.label}</span>)}
    </span>
  )
}

// FilePanel shows the downloaded file name + its metadata chips.
export function FilePanel({ file }: { file: FileMeta }) {
  return (
    <div className="file-panel">
      <div className="file-name"><Icon name="film" /><span title={file.path || file.name}>{file.name}</span></div>
      <MetaChips file={file} />
    </div>
  )
}
