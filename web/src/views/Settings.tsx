import { useEffect, useState } from 'react'
import { getJSON, putJSON, postJSON } from '../api'
import { Icon, Loading, ErrorBanner } from '../ui'

interface SettingsResponse {
  settings: Record<string, string> // DB overlay (secrets masked as ********)
  effective: Record<string, string> // effective non-secret values
  configured: Record<string, boolean>
}

// Field groups mirror the settings keys (internal/settings). secret fields render
// as password inputs and show a "configured" hint instead of the value.
const groups: { title: string; fields: { key: string; label: string; secret?: boolean; bool?: boolean }[] }[] = [
  {
    title: 'TorBox',
    fields: [
      { key: 'torbox.token', label: 'API token', secret: true },
      { key: 'torbox.webdav_user', label: 'WebDAV user' },
      { key: 'torbox.webdav_pass', label: 'WebDAV password', secret: true },
    ],
  },
  {
    title: 'Prowlarr',
    fields: [
      { key: 'prowlarr.url', label: 'URL' },
      { key: 'prowlarr.api_key', label: 'API key', secret: true },
    ],
  },
  { title: 'TMDB', fields: [{ key: 'tmdb.token', label: 'API read token', secret: true }] },
  {
    title: 'TVDB',
    fields: [
      { key: 'tvdb.api_key', label: 'API key', secret: true },
      { key: 'tvdb.pin', label: 'PIN', secret: true },
    ],
  },
  {
    title: 'Plex',
    fields: [
      { key: 'plex.url', label: 'URL' },
      { key: 'plex.token', label: 'Token', secret: true },
      { key: 'plex.movie_section', label: 'Movie section id' },
      { key: 'plex.tv_section', label: 'TV section id' },
    ],
  },
  { title: 'Seerr (Overseerr/Jellyseerr)', fields: [{ key: 'seerr.api_keys', label: 'API keys (comma-separated)', secret: true }] },
  {
    title: 'Library & WebDAV',
    fields: [
      { key: 'webdav.mount_root', label: 'WebDAV mount root' },
      { key: 'webdav.usenet_subpath', label: 'Usenet subpath' },
      { key: 'webdav.torrent_subpath', label: 'Torrent subpath' },
      { key: 'library.movie_root', label: 'Movie library root' },
      { key: 'library.tv_root', label: 'TV library root' },
    ],
  },
  {
    title: 'Intervals & automation',
    fields: [
      { key: 'interval.poll', label: 'Poll interval (e.g. 1m)' },
      { key: 'interval.reconcile', label: 'Reconcile interval (e.g. 15m)' },
      { key: 'interval.metadata', label: 'Metadata refresh (e.g. 24h)' },
      { key: 'interval.search', label: 'Auto-search interval (e.g. 6h)' },
      { key: 'automation.enabled', label: 'Automation enabled', bool: true },
    ],
  },
  {
    title: 'App',
    fields: [{ key: 'api.key', label: 'UI API key (blank = open on localhost)', secret: true }],
  },
  {
    title: 'Release selection — filters',
    fields: [
      { key: 'select.allowed_resolutions', label: 'Allowed resolutions (csv; empty = all)' },
      { key: 'select.preferred_resolutions', label: 'Preferred resolutions (csv, best first)' },
      { key: 'select.preferred_qualities', label: 'Preferred qualities (csv, best first)' },
      { key: 'select.min_seeders', label: 'Min seeders (torrents)' },
      { key: 'select.min_grabs', label: 'Min grabs (usenet)' },
      { key: 'select.require_cached', label: 'Require cached (torrents only)', bool: true },
      { key: 'select.min_size', label: 'Min size (bytes; 0 = unbounded)' },
      { key: 'select.max_size', label: 'Max size (bytes; 0 = unbounded)' },
      { key: 'select.min_score', label: 'Min score (reject below)' },
      { key: 'select.blocked_keywords', label: 'Blocked keywords (csv)' },
      { key: 'select.blocked_groups', label: 'Blocked groups (csv)' },
      { key: 'select.preferred_keywords', label: 'Preferred keywords (csv)' },
      { key: 'select.preferred_groups', label: 'Preferred groups (csv)' },
      { key: 'select.size_limits', label: 'Per-quality size limits (JSON)' },
    ],
  },
  {
    title: 'Release selection — weights',
    fields: [
      { key: 'select.weight_resolution', label: 'Resolution weight' },
      { key: 'select.weight_quality', label: 'Quality weight' },
      { key: 'select.weight_protocol_cached_torrent', label: 'Cached-torrent weight' },
      { key: 'select.weight_protocol_usenet', label: 'Usenet weight' },
      { key: 'select.weight_protocol_uncached_torrent', label: 'Uncached-torrent weight' },
      { key: 'select.weight_health', label: 'Health weight' },
      { key: 'select.seed_saturation', label: 'Seed saturation (health divisor)' },
      { key: 'select.weight_preferred_group', label: 'Preferred-group bonus' },
      { key: 'select.weight_preferred_keyword', label: 'Preferred-keyword bonus' },
      { key: 'select.weight_freeleech', label: 'Freeleech bonus' },
      { key: 'select.weight_proper', label: 'Proper/Repack bonus' },
    ],
  },
]

// Maps a settings group title to a test service + the request body built from
// the current values (posted to /settings/test/{svc} for test-before-save).
const groupService: Record<string, { svc: string; body: (v: (k: string) => string) => Record<string, string> }> = {
  TorBox: { svc: 'torbox', body: (v) => ({ token: v('torbox.token') }) },
  Prowlarr: { svc: 'prowlarr', body: (v) => ({ url: v('prowlarr.url'), apiKey: v('prowlarr.api_key') }) },
  TMDB: { svc: 'tmdb', body: (v) => ({ token: v('tmdb.token') }) },
  TVDB: { svc: 'tvdb', body: (v) => ({ apiKey: v('tvdb.api_key'), pin: v('tvdb.pin') }) },
  Plex: { svc: 'plex', body: (v) => ({ url: v('plex.url'), token: v('plex.token') }) },
}

export function Settings() {
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [edits, setEdits] = useState<Record<string, string>>({})
  const [tests, setTests] = useState<Record<string, string>>({})
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  function reload() {
    getJSON<SettingsResponse>('/settings')
      .then((d) => { setData(d); setEdits({}) })
      .catch((e: unknown) => setErr(String(e)))
  }
  useEffect(reload, [])

  if (err) return <ErrorBanner message={err} />
  if (!data) return <Loading />

  function valueOf(key: string, secret?: boolean): string {
    if (key in edits) return edits[key]
    if (secret) return '' // never prefill secrets
    return data!.effective[key] ?? data!.settings[key] ?? ''
  }

  async function save() {
    setMsg('Saving…')
    try {
      const updated = await putJSON<SettingsResponse>('/settings', { settings: edits })
      setData(updated)
      setEdits({})
      setMsg('Saved — applied immediately (no restart).')
    } catch (e) {
      setMsg('Save failed: ' + String(e))
    }
  }

  async function test(svc: string, body: Record<string, string>) {
    setTests({ ...tests, [svc]: 'testing…' })
    try {
      const r = await postJSON<{ ok: boolean; detail: string }>(`/settings/test/${svc}`, body)
      setTests((t) => ({ ...t, [svc]: (r.ok ? '✓ ' : '✗ ') + r.detail }))
    } catch (e) {
      setTests((t) => ({ ...t, [svc]: '✗ ' + String(e) }))
    }
  }

  const dirty = Object.keys(edits).length > 0

  return (
    <section>
      <p className="muted" style={{ marginTop: 0, marginBottom: 18 }}>
        Everything Boxarr needs is configured here — no environment variables required. Changes apply immediately.
      </p>
      <div className="settings-grid">
        {groups.map((g) => {
          const gs = groupService[g.title]
          const svcStatus = gs ? data!.configured[gs.svc] : undefined
          return (
            <div key={g.title} className="fieldset">
              <div className="legend" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                {g.title}
                {svcStatus !== undefined && (
                  <span className={`status ${svcStatus ? 'available' : 'missing'}`} style={{ marginLeft: 'auto' }}>
                    {svcStatus ? 'connected' : 'not set'}
                  </span>
                )}
              </div>
              {g.fields.map((f) => (
                <div key={f.key} className={f.bool ? 'field field-row' : 'field'}>
                  {f.bool ? (
                    <>
                      <input id={f.key} type="checkbox" checked={valueOf(f.key) === 'true'}
                        onChange={(e) => setEdits({ ...edits, [f.key]: e.target.checked ? 'true' : 'false' })} />
                      <label htmlFor={f.key} style={{ margin: 0 }}>{f.label}</label>
                    </>
                  ) : (
                    <>
                      <label htmlFor={f.key}>{f.label}</label>
                      <input id={f.key} className="input" type={f.secret ? 'password' : 'text'}
                        placeholder={f.secret ? secretPlaceholder(f.key, data!) : ''}
                        value={valueOf(f.key, f.secret)}
                        onChange={(e) => setEdits({ ...edits, [f.key]: e.target.value })} />
                    </>
                  )}
                </div>
              ))}
              {gs && (
                <div className="test-line">
                  {/* Send only edited values; the server falls back to saved ones
                      (so unedited secrets are never sent as the redacted mask). */}
                  <button className="btn btn-sm" onClick={() => void test(gs.svc, gs.body((k) => edits[k] ?? ''))}>
                    <Icon name="refresh" /> Test connection
                  </button>
                  <span className={testTone(tests[gs.svc])}>{tests[gs.svc] ?? ''}</span>
                </div>
              )}
            </div>
          )
        })}
      </div>
      <div className="save-bar">
        <button className="btn btn-primary" onClick={() => void save()} disabled={!dirty}>
          <Icon name="check" /> Save changes
        </button>
        {dirty && !msg && <span className="toast">{Object.keys(edits).length} unsaved</span>}
        {msg && <span className={`toast${msg.startsWith('Saved') ? ' ok' : ''}`}>{msg}</span>}
      </div>
    </section>
  )
}

function testTone(v?: string): string {
  if (!v) return ''
  if (v.startsWith('✓')) return 'test-ok'
  if (v.startsWith('✗')) return 'test-bad'
  return ''
}

function secretPlaceholder(key: string, data: SettingsResponse): string {
  return data.settings[key] === '********' ? '•••••••• (set)' : ''
}
