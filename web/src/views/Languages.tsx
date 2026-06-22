import { useEffect, useMemo, useState } from 'react'
import { getJSON } from '../api'
import { Icon, Loading, ErrorBanner, Empty } from '../ui'

interface RL {
  releaseName: string
  releaseGroup: string
  audioLangs: string // csv of 2-letter codes
  subLangs: string
  source: string
}

// Languages is the verified release→language knowledge base: the real audio +
// subtitle languages observed (via the Plex stream check) per downloaded release,
// searchable, with a summary of which release groups ship each language.
export function Languages() {
  const [items, setItems] = useState<RL[] | null>(null)
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')

  useEffect(() => {
    getJSON<{ items: RL[] }>('/releases/languages')
      .then((r) => setItems(r.items ?? []))
      .catch((e: unknown) => setErr(String(e)))
  }, [])

  // Groups → set of languages they've been verified to ship (audio or subs).
  const groupLangs = useMemo(() => {
    const m: Record<string, Set<string>> = {}
    for (const it of items ?? []) {
      if (!it.releaseGroup) continue
      const set = (m[it.releaseGroup] ??= new Set())
      for (const l of splitCsv(it.audioLangs)) set.add(l)
      for (const l of splitCsv(it.subLangs)) set.add(l)
    }
    return m
  }, [items])

  if (err) return <ErrorBanner message={err} />
  if (!items) return <Loading />

  const ql = q.trim().toLowerCase()
  const shown = items.filter((it) =>
    !ql ||
    it.releaseName.toLowerCase().includes(ql) ||
    it.releaseGroup.toLowerCase().includes(ql) ||
    it.audioLangs.toLowerCase().includes(ql) ||
    it.subLangs.toLowerCase().includes(ql),
  )
  const germanGroups = Object.keys(groupLangs).filter((g) => groupLangs[g].has('de')).sort()

  return (
    <section>
      <div className="row-between" style={{ marginBottom: 14, gap: 12, flexWrap: 'wrap' }}>
        <span className="muted">{items.length} verified release{items.length === 1 ? '' : 's'} · {Object.keys(groupLangs).length} groups</span>
        <div className="search-box">
          <Icon name="search" />
          <input className="input" type="search" placeholder="Filter by release, group, or language…" value={q}
            onChange={(e) => setQ(e.target.value)} />
        </div>
      </div>

      {germanGroups.length > 0 && (
        <div className="lang-summary">
          <span className="muted">Groups verified to ship German:</span>
          {germanGroups.map((g) => <span key={g} className="meta-chip lang">{g}</span>)}
        </div>
      )}

      {items.length === 0 ? (
        <Empty icon="languages" title="No verified languages yet"
          hint="After Boxarr checks a download's tracks in Plex, the real audio + subtitle languages are recorded here." />
      ) : (
        <div className="table-wrap">
          <table className="tbl">
            <thead><tr><th>Release</th><th style={{ width: 140 }}>Group</th><th style={{ width: 150 }}>Audio</th><th style={{ width: 150 }}>Subtitles</th><th style={{ width: 80 }}>Source</th></tr></thead>
            <tbody>
              {shown.map((it) => (
                <tr key={it.releaseName}>
                  <td className="rel-title">{it.releaseName}</td>
                  <td className="muted">{it.releaseGroup || '—'}</td>
                  <td><LangChips csv={it.audioLangs} /></td>
                  <td><LangChips csv={it.subLangs} /></td>
                  <td className="muted" style={{ fontSize: 12 }}>{it.source}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function LangChips({ csv }: { csv: string }) {
  const langs = splitCsv(csv)
  if (langs.length === 0) return <span className="muted">—</span>
  return <span className="lang-cell">{langs.map((l) => <span key={l} className={`meta-chip lang${l === 'de' ? ' de' : ''}`}>{l}</span>)}</span>
}

function splitCsv(s: string): string[] {
  return s ? s.split(',').map((x) => x.trim()).filter(Boolean) : []
}
