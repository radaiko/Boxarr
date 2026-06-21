import { useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { type Release } from '../api'
import { Icon } from '../ui'
import { ReleaseTable } from './ReleaseTable'

// SearchOverlay shows ranked releases in a modal with filtering (resolution,
// language, subtitles, cached, hide-rejected). currentName marks the release
// already grabbed for the item so you can see the prior decision without
// re-evaluating — the search itself is still fresh.
export function SearchOverlay({ title, releases, currentName, onGrab, onClose, busy }: {
  title: string
  releases: Release[]
  currentName?: string
  onGrab: (r: Release) => void
  onClose: () => void
  busy?: boolean
}) {
  const [res, setRes] = useState('')
  const [lang, setLang] = useState('')
  const [subsOnly, setSubsOnly] = useState(false)
  const [cachedOnly, setCachedOnly] = useState(false)
  const [hideRejected, setHideRejected] = useState(true)

  const resolutions = useMemo(() => uniq(releases.map((r) => r.resolution).filter(Boolean) as string[]), [releases])
  const langs = useMemo(() => uniq(releases.flatMap((r) => r.languages ?? [])), [releases])

  const shown = releases.filter((r) =>
    (!res || r.resolution === res) &&
    (!lang || (r.languages ?? []).includes(lang)) &&
    (!subsOnly || r.subs) &&
    (!cachedOnly || r.cached === true) &&
    (!hideRejected || !r.rejected),
  )

  return createPortal(
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-wide" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Releases">
        <div className="modal-head">
          <span className="modal-title">Releases · {title}</span>
          <span className="muted" style={{ fontSize: 12 }}>{shown.length}/{releases.length}</span>
          <button className="btn-icon" aria-label="Close" title="Close" style={{ marginLeft: 'auto' }} onClick={onClose}><Icon name="back" /></button>
        </div>
        <div className="modal-body">
          <div className="filter-bar">
            <select className="input" value={res} onChange={(e) => setRes(e.target.value)} aria-label="Resolution">
              <option value="">Any resolution</option>
              {resolutions.map((x) => <option key={x} value={x}>{x}</option>)}
            </select>
            <select className="input" value={lang} onChange={(e) => setLang(e.target.value)} aria-label="Language">
              <option value="">Any language</option>
              {langs.map((x) => <option key={x} value={x}>{x}</option>)}
            </select>
            <label className="chk"><input type="checkbox" checked={subsOnly} onChange={(e) => setSubsOnly(e.target.checked)} /> Subs</label>
            <label className="chk"><input type="checkbox" checked={cachedOnly} onChange={(e) => setCachedOnly(e.target.checked)} /> Cached only</label>
            <label className="chk"><input type="checkbox" checked={hideRejected} onChange={(e) => setHideRejected(e.target.checked)} /> Hide rejected</label>
          </div>
          {busy
            ? <p className="muted" style={{ padding: '12px 2px' }}>Searching…</p>
            : <ReleaseTable releases={shown} onGrab={onGrab} currentName={currentName} />}
        </div>
      </div>
    </div>,
    document.body,
  )
}

function uniq(xs: string[]): string[] { return Array.from(new Set(xs)).sort() }
