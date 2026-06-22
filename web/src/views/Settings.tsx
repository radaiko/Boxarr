import { useEffect, useState } from 'react'
import { getJSON, putJSON, postJSON, setApiKey, getApiKey } from '../api'
import { toast } from '../toast'
import { Icon, Loading, ErrorBanner, initials } from '../ui'
import { PlexWizard } from './PlexWizard'

interface SettingsResponse {
  settings: Record<string, string> // DB overlay (secrets masked as ********)
  effective: Record<string, string> // effective non-secret values
  configured: Record<string, boolean>
}

// Field groups mirror the settings keys (internal/settings). secret fields render
// as password inputs and show a "configured" hint instead of the value.
const groups: { title: string; fields: { key: string; label: string; secret?: boolean; bool?: boolean; placeholder?: string }[] }[] = [
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
      { key: 'prowlarr.url', label: 'Server URL', placeholder: 'http://prowlarr:9696' },
      { key: 'prowlarr.api_key', label: 'API key', secret: true },
    ],
  },
  { title: 'TMDB', fields: [{ key: 'tmdb.token', label: 'API read token (v4)', secret: true }] },
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
      { key: 'plex.url', label: 'Server URL', placeholder: 'http://plex:32400' },
      { key: 'plex.token', label: 'X-Plex-Token', secret: true },
      { key: 'plex.movie_section', label: 'Movie library section ID', placeholder: '1' },
      { key: 'plex.tv_section', label: 'TV library section ID', placeholder: '2' },
      { key: 'plex.anime_section', label: 'Anime library section ID', placeholder: '3' },
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
      { key: 'library.anime_root', label: 'Anime library root' },
    ],
  },
  {
    title: 'Intervals & automation',
    fields: [
      { key: 'interval.poll', label: 'Poll interval (e.g. 1m)' },
      { key: 'interval.reconcile', label: 'Reconcile interval (e.g. 15m)' },
      { key: 'interval.metadata', label: 'Metadata refresh (e.g. 24h)' },
      { key: 'interval.search', label: 'Auto-search tick (e.g. 1h)' },
      { key: 'automation.enabled', label: 'Automation enabled', bool: true },
      { key: 'automation.upgrade_enabled', label: 'Upgrade to better language/quality', bool: true },
      { key: 'plex.auto_language', label: 'Auto-set Plex audio/subtitle language', bool: true },
      { key: 'search.cadence_fast_window', label: 'Cadence — fast window after release (e.g. 48h)' },
      { key: 'search.cadence_fast_interval', label: 'Cadence — fast interval (e.g. 1h)' },
      { key: 'search.cadence_daily_window', label: 'Cadence — daily window (e.g. 336h = 2w)' },
      { key: 'search.cadence_daily_interval', label: 'Cadence — daily interval (e.g. 24h)' },
      { key: 'search.cadence_slow_interval', label: 'Cadence — slow interval after that (e.g. 720h = 30d)' },
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
  {
    title: 'Release selection — languages',
    fields: [
      { key: 'select.movie_lang_required', label: 'Movies — required languages', placeholder: 'DE' },
      { key: 'select.movie_lang_preferred', label: 'Movies — preferred (boost)', placeholder: 'EN' },
      { key: 'select.series_lang_required', label: 'Series — required languages', placeholder: 'DE' },
      { key: 'select.series_lang_preferred', label: 'Series — preferred (boost)', placeholder: 'EN' },
      { key: 'select.anime_lang_required', label: 'Anime — required languages', placeholder: 'DE,EN' },
      { key: 'select.anime_lang_preferred', label: 'Anime — preferred (boost)', placeholder: 'EN' },
      { key: 'select.anime_require_any', label: 'Anime: any one required language is enough', bool: true },
      { key: 'select.anime_prefer_subs', label: 'Anime: prefer English subtitles', bool: true },
      { key: 'select.weight_language', label: 'Preferred-language bonus' },
      { key: 'select.weight_subs', label: 'English-subs bonus' },
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

// One-line role per connection (shown under the name, Sonarr-style).
const ROLES: Record<string, string> = {
  TorBox: 'Download backend',
  Prowlarr: 'Indexer search',
  TMDB: 'Movie & TV metadata',
  TVDB: 'TV metadata (scene / absolute ordering)',
  Plex: 'Library / media server',
}

// Tabs group the settings like Sonarr/Radarr instead of one long page.
const TABS: { id: string; groups: string[] }[] = [
  { id: 'Connections', groups: ['TorBox', 'Prowlarr', 'TMDB', 'TVDB', 'Plex'] },
  { id: 'Requests', groups: [] }, // special Seerr key panel
  { id: 'Library', groups: ['Library & WebDAV'] },
  { id: 'Downloads', groups: ['Intervals & automation'] },
  { id: 'Selection', groups: ['Release selection — filters', 'Release selection — weights', 'Release selection — languages'] },
  { id: 'General', groups: [] }, // special web-UI key panel
]
const byTitle = Object.fromEntries(groups.map((g) => [g.title, g]))

export function Settings() {
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [edits, setEdits] = useState<Record<string, string>>({})
  const [tests, setTests] = useState<Record<string, TestState>>({})
  const [tab, setTab] = useState('Connections')
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
    if (secret) return ''
    return data!.effective[key] ?? data!.settings[key] ?? ''
  }

  async function save() {
    setMsg('Saving…')
    try {
      if ('api.key' in edits) setApiKey(edits['api.key'])
      const updated = await putJSON<SettingsResponse>('/settings', { settings: edits })
      setData(updated); setEdits({}); setMsg('Saved.'); toast('Settings saved.', 'ok')
    } catch (e) { setMsg('Save failed: ' + String(e)); toast('Save failed: ' + String(e), 'err') }
  }

  async function test(svc: string, body: Record<string, string>) {
    setTests((t) => ({ ...t, [svc]: 'pending' }))
    try {
      const r = await postJSON<{ ok: boolean; detail: string }>(`/settings/test/${svc}`, body)
      setTests((t) => ({
        ...t,
        [svc]: { ok: r.ok, text: r.ok ? (r.detail || 'Connected') : `Couldn’t connect — ${r.detail || 'check the values above'}` },
      }))
    } catch {
      setTests((t) => ({ ...t, [svc]: { ok: false, text: 'Couldn’t connect — no response from that URL' } }))
    }
  }

  // Persist a single key immediately (used by the one-click key generators).
  async function setOne(key: string, value: string) {
    setMsg('Saving…')
    try {
      if (key === 'api.key') setApiKey(value)
      const updated = await putJSON<SettingsResponse>('/settings', { settings: { [key]: value } })
      setData(updated); setMsg('Saved.')
    } catch (e) { setMsg('Save failed: ' + String(e)) }
  }

  function renderGroup(g: typeof groups[number]) {
    const gs = groupService[g.title]
    const svcStatus = gs ? data!.configured[gs.svc] : undefined
    const role = ROLES[g.title]
    const id = `fs-${g.title.replace(/\W+/g, '-')}`
    const ts = gs ? tests[gs.svc] : undefined
    return (
      <div key={g.title} className="fieldset" role="group" aria-labelledby={id}>
        <div className="fs-head">
          <span className="fs-tile" aria-hidden="true">{initials(g.title)}</span>
          <div className="fs-id">
            <span className="fs-name" id={id}>{g.title}</span>
            {role && <span className="fs-role">{role}</span>}
          </div>
          {svcStatus !== undefined && (
            <span className={`status ${svcStatus ? 'available' : 'idle'}`}>{svcStatus ? 'connected' : 'not set'}</span>
          )}
        </div>
        {g.title === 'Plex' && (
          <PlexWizard effective={data!.effective} configured={!!data!.configured.plex} onChange={reload} />
        )}
        {g.title === 'Plex' && <p className="hint-line">Or enter the server details manually:</p>}
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
                  placeholder={f.secret ? secretPlaceholder(f.key, data!) : (f.placeholder ?? '')}
                  value={valueOf(f.key, f.secret)}
                  onChange={(e) => setEdits({ ...edits, [f.key]: e.target.value })} />
              </>
            )}
          </div>
        ))}
        {gs && (
          <div className="test-line">
            <button className="btn" onClick={() => void test(gs.svc, gs.body((k) => edits[k] ?? ''))}>
              <Icon name="refresh" /> Test connection
            </button>
            <span role="status" aria-live="polite" className={testTone(ts)}>
              {ts === 'pending' ? 'Testing…' : (ts ? ts.text : '')}
            </span>
          </div>
        )}
      </div>
    )
  }

  const seerrKey = edits['seerr.api_keys'] ?? data.effective['seerr.api_keys'] ?? ''
  const appKey = getApiKey()
  const dirty = Object.keys(edits).length > 0
  const active = TABS.find((t) => t.id === tab) ?? TABS[0]

  async function runAction(path: string, what: string) {
    try {
      await postJSON(path, {})
      toast(`${what} started — follow it on the Activity page.`, 'ok')
    } catch (e) {
      toast(`${what} failed: ${String(e)}`, 'err')
    }
  }

  return (
    <section>
      <div className="tabs">
        {TABS.map((t) => (
          <button key={t.id} className={`tab${t.id === tab ? ' active' : ''}`}
            onClick={() => { setTab(t.id); setMsg('') }}>{t.id}</button>
        ))}
      </div>

      {tab === 'Requests' ? (
        <div className="fieldset" style={{ maxWidth: 720 }}>
          <div className="legend">Requests · Overseerr / Jellyseerr</div>
          <p className="muted" style={{ marginTop: 0 }}>
            Boxarr emulates Sonarr &amp; Radarr so Seerr can send requests straight in. Seerr authenticates with this one key — generate it here, then paste it into Seerr.
          </p>
          {seerrKey ? (
            <div className="keybox">
              <code>{seerrKey}</code>
              <button className="btn btn-sm" onClick={() => void copy(seerrKey)}><Icon name="copy" /> Copy</button>
              <button className="btn btn-sm" onClick={() => void setOne('seerr.api_keys', genKey())}><Icon name="refresh" /> Regenerate</button>
            </div>
          ) : (
            <button className="btn btn-primary" onClick={() => void setOne('seerr.api_keys', genKey())}>
              <Icon name="key" /> Generate API key
            </button>
          )}
          <div className="hint-block">
            In <strong>Seerr → Settings → Services</strong>, add both with this key:<br />
            Sonarr → Server <code>http://boxarr:8080/sonarr</code><br />
            Radarr → Server <code>http://boxarr:8080/radarr</code><br />
            <span className="muted">Use Boxarr’s address on your Docker network (the service/container name), not the browser URL.</span>
          </div>
          {msg && <p className={`toast${msg === 'Saved.' ? ' ok' : ''}`} style={{ marginTop: 14 }}>{msg}</p>}
        </div>
      ) : tab === 'General' ? (
        <div className="fieldset" style={{ maxWidth: 720 }}>
          <div className="legend">Web UI access</div>
          <p className="muted" style={{ marginTop: 0 }}>
            Secures Boxarr’s own web UI &amp; API. Leave it open on a trusted LAN, or set a key before exposing Boxarr. This is <strong>not</strong> the key Seerr uses.
          </p>
          <div className="keybox">
            <code>{appKey || '(open — anyone on the network can access)'}</code>
            {appKey && <button className="btn btn-sm" onClick={() => void copy(appKey)}><Icon name="copy" /> Copy</button>}
            <button className="btn btn-sm" onClick={() => void setOne('api.key', genKey())}>
              <Icon name="key" /> {appKey ? 'Regenerate' : 'Generate key'}
            </button>
            {appKey && <button className="btn btn-sm btn-danger" onClick={() => void setOne('api.key', '')}>Remove</button>}
          </div>
          {msg && <p className={`toast${msg === 'Saved.' ? ' ok' : ''}`} style={{ marginTop: 14 }}>{msg}</p>}
        </div>
      ) : (
        <>
          <div className={tab === 'Connections' ? 'settings-stack' : 'settings-grid'}>
            {active.groups.map((title) => byTitle[title] && renderGroup(byTitle[title]))}
          </div>
          {tab === 'Downloads' && (
            <div className="field-row" style={{ gap: 8, marginTop: 14, flexWrap: 'wrap' }}>
              <button className="btn btn-sm" onClick={() => void runAction('/upgrade/search', 'Upgrade search')}>
                <Icon name="refresh" /> Search for upgrades now
              </button>
              <button className="btn btn-sm" onClick={() => void runAction('/plex/language-sweep', 'Plex language update')}>
                <Icon name="refresh" /> Update Plex languages now
              </button>
            </div>
          )}
          <div className="save-bar">
            <button className="btn btn-primary" onClick={() => void save()} disabled={!dirty}>
              <Icon name="check" /> Save changes
            </button>
            {dirty && msg === '' && <span className="toast">{Object.keys(edits).length} unsaved</span>}
            {msg && <span className={`toast${msg === 'Saved.' ? ' ok' : ''}`}>{msg}</span>}
          </div>
        </>
      )}
    </section>
  )
}

function genKey(): string {
  const a = new Uint8Array(24)
  crypto.getRandomValues(a)
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('')
}

function copy(text: string): void {
  void navigator.clipboard?.writeText(text)
}

type TestState = 'pending' | { ok: boolean; text: string }

function testTone(t?: TestState): string {
  if (!t || t === 'pending') return ''
  return t.ok ? 'test-ok' : 'test-bad'
}

function secretPlaceholder(key: string, data: SettingsResponse): string {
  return data.settings[key] === '********' ? '•••••••• (set)' : ''
}
