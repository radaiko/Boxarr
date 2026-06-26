import { useEffect, useMemo, useState } from 'react'
import { getJSON, del } from '../api'
import { Icon, Loading, ErrorBanner, Empty } from '../ui'
import { toast } from '../toast'

interface RL {
  releaseName: string
  releaseGroup: string
  audioLangs: string // csv of 2-letter codes
  subLangs: string
  source: string
}

interface GroupStat {
  group: string
  total: number
  inLang: number
  ratio: number
  trusted: boolean // ≥90% of the group's releases ship the language (gets a scoring boost)
}

interface BlocklistEntry {
  releaseName: string
  reason: string
  createdAt: string
}

interface LangResponse {
  items: RL[]
  favoriteLangs?: string[]
  groupStats?: Record<string, GroupStat[]>
  blocklist?: BlocklistEntry[]
}

// Languages is the verified release→language knowledge base: the real audio +
// subtitle languages observed (via the Plex stream check) per downloaded release,
// searchable, plus per-group reliability for the favorited languages (the groups
// that most often ship the right language — and which scoring now boosts).
export function Languages() {
  const [items, setItems] = useState<RL[] | null>(null)
  const [favs, setFavs] = useState<string[]>([])
  const [groupStats, setGroupStats] = useState<Record<string, GroupStat[]>>({})
  const [blocklist, setBlocklist] = useState<BlocklistEntry[]>([])
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')

  function load() {
    getJSON<LangResponse>('/releases/languages')
      .then((r) => {
        setItems(r.items ?? [])
        setFavs(r.favoriteLangs ?? [])
        setGroupStats(r.groupStats ?? {})
        setBlocklist(r.blocklist ?? [])
      })
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(load, [])

  async function unblock(rel: string) {
    try {
      await del('/releases/blocklist?name=' + encodeURIComponent(rel))
      toast('Removed from the failed-release blocklist — it can be grabbed again.', 'ok')
      setBlocklist((b) => b.filter((e) => e.releaseName !== rel))
    } catch (e) {
      toast(`Couldn't remove: ${String(e)}`, 'err')
    }
  }

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
  const favStats = favs.filter((l) => (groupStats[l]?.length ?? 0) > 0)

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

      {favStats.length > 0 && (
        <div className="group-reliability" style={{ marginBottom: 18 }}>
          {favStats.map((lang) => (
            <GroupReliability key={lang} lang={lang} stats={groupStats[lang]} />
          ))}
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

      {blocklist.length > 0 && (
        <div style={{ marginTop: 22 }}>
          <div className="muted" style={{ marginBottom: 6, fontSize: 13 }}>
            Failed releases <span className="status broken" style={{ marginLeft: 6 }}>{blocklist.length}</span>
            <span style={{ fontSize: 11, opacity: 0.7, marginLeft: 6 }}>
              — blocklisted after a failed download; skipped on re-search. Remove to allow grabbing again.
            </span>
          </div>
          <div className="table-wrap">
            <table className="tbl">
              <thead><tr><th>Release</th><th style={{ width: 240 }}>Reason</th><th style={{ width: 150 }}>When</th><th className="grab-col" /></tr></thead>
              <tbody>
                {blocklist.map((b) => (
                  <tr key={b.releaseName}>
                    <td className="rel-title">{b.releaseName}</td>
                    <td className="muted" style={{ fontSize: 12 }}>{b.reason || '—'}</td>
                    <td className="muted" style={{ fontSize: 12 }}>{b.createdAt}</td>
                    <td><button className="btn btn-sm" title="Remove from blocklist (allow grabbing again)"
                      onClick={() => void unblock(b.releaseName)}><Icon name="trash" /> Remove</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}

// GroupReliability shows, for one favorited language, which release groups most
// often ship it (count + ratio). Groups ≥90% over ≥3 releases are "trusted" and
// get a likelihood boost in search scoring.
function GroupReliability({ lang, stats }: { lang: string; stats: GroupStat[] }) {
  const top = stats.slice(0, 30)
  return (
    <div style={{ marginBottom: 14 }}>
      <div className="muted" style={{ marginBottom: 6, fontSize: 13 }}>
        Groups by <span className={`meta-chip lang${lang === 'de' ? ' de' : ''}`}>{lang}</span> reliability
        <span style={{ fontSize: 11, opacity: 0.7, marginLeft: 6 }}>✓ trusted = boosted in search (≥90% over ≥3 releases)</span>
      </div>
      <div className="table-wrap">
        <table className="tbl">
          <thead><tr>
            <th>Group</th>
            <th style={{ width: 110 }}>{lang.toUpperCase()} / total</th>
            <th style={{ width: 180 }}>Reliability</th>
          </tr></thead>
          <tbody>
            {top.map((g) => (
              <tr key={g.group}>
                <td className="muted">
                  {g.group}
                  {g.trusted && <span className="chip instant" style={{ marginLeft: 8 }}>✓ trusted</span>}
                </td>
                <td>{g.inLang} / {g.total}</td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <div style={{ flex: 1, height: 6, background: '#2a2f3a', borderRadius: 3, overflow: 'hidden' }}>
                      <div style={{ width: `${Math.round(g.ratio * 100)}%`, height: '100%', background: g.trusted ? '#3fb950' : '#6b7280' }} />
                    </div>
                    <span style={{ fontSize: 12, minWidth: 34, textAlign: 'right' }}>{Math.round(g.ratio * 100)}%</span>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
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
