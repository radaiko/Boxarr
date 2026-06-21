import { useEffect, useState } from 'react'
import { getJSON, putJSON, postJSON } from '../api'

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
]

const testable = ['torbox', 'prowlarr', 'tmdb', 'tvdb', 'plex'] as const

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

  if (err) return <p>Error: {err}</p>
  if (!data) return <p>Loading…</p>

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

  return (
    <section>
      <h2>Settings</h2>
      <p>
        Configure all connections here — no environment variables required. Changes apply immediately.
      </p>
      {groups.map((g) => (
        <fieldset key={g.title} style={{ marginBottom: 12 }}>
          <legend>{g.title}</legend>
          {g.fields.map((f) => (
            <div key={f.key} style={{ margin: '4px 0' }}>
              <label style={{ display: 'inline-block', width: 220 }}>{f.label}</label>
              {f.bool ? (
                <input
                  type="checkbox"
                  checked={valueOf(f.key) === 'true'}
                  onChange={(e) => setEdits({ ...edits, [f.key]: e.target.checked ? 'true' : 'false' })}
                />
              ) : (
                <input
                  type={f.secret ? 'password' : 'text'}
                  style={{ width: 320 }}
                  placeholder={f.secret && data!.configured ? secretPlaceholder(f.key, data!) : ''}
                  value={valueOf(f.key, f.secret)}
                  onChange={(e) => setEdits({ ...edits, [f.key]: e.target.value })}
                />
              )}
            </div>
          ))}
          {groupService[g.title] && (
            <div style={{ marginTop: 4 }}>
              <button
                onClick={() => {
                  // Send only edited values; the server falls back to saved ones
                  // (so unedited secrets are never sent as the redacted mask).
                  const gs = groupService[g.title]
                  void test(gs.svc, gs.body((k) => edits[k] ?? ''))
                }}
              >
                Test connection
              </button>{' '}
              <span>{tests[groupService[g.title].svc] ?? ''}</span>
            </div>
          )}
        </fieldset>
      ))}
      <button onClick={() => void save()} disabled={Object.keys(edits).length === 0}>
        Save
      </button>{' '}
      {msg && <span>{msg}</span>}
      <h3>Connection status</h3>
      <ul>
        {testable.map((svc) => (
          <li key={svc}>
            {svc}: {data.configured[svc] ? 'configured' : 'not configured'}
          </li>
        ))}
      </ul>
    </section>
  )
}

function secretPlaceholder(key: string, data: SettingsResponse): string {
  return data.settings[key] === '********' ? '•••••••• (set)' : ''
}
