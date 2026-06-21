import { useState } from 'react'
import { getJSON, postJSON, putJSON } from '../api'
import { Icon } from '../ui'

interface Server { name: string; uri: string; uris: string[] }
interface Section { key: string; title: string; type: string }
interface Effective { 'plex.url'?: string; 'plex.movie_section'?: string; 'plex.tv_section'?: string; 'plex.anime_section'?: string }

// PlexWizard: official Plex login (PIN OAuth) → pick a server → map libraries to
// Movies / Series / Anime, saving each choice immediately. Falls back to the
// manual URL/token/section fields rendered below it.
export function PlexWizard({ effective, configured, onChange }: {
  effective: Effective
  configured: boolean
  onChange: () => void
}) {
  const [servers, setServers] = useState<Server[] | null>(null)
  const [sections, setSections] = useState<Section[] | null>(null)
  const [url, setUrl] = useState(effective['plex.url'] ?? '')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')

  async function save(key: string, value: string) {
    await putJSON('/settings', { settings: { [key]: value } })
    onChange()
  }

  async function signIn() {
    setBusy(true); setMsg('Opening Plex…')
    try {
      const pin = await postJSON<{ id: number; code: string; authUrl: string }>('/plex/pin', {})
      window.open(pin.authUrl, 'plex-auth', 'width=800,height=720')
      setMsg('Waiting for you to authorize in the Plex window…')
      const ok = await pollPin(pin.id)
      if (!ok) { setMsg('Timed out — try Sign in again.'); return }
      setMsg('Signed in. Loading your servers…')
      await loadServers()
      setMsg('')
    } catch (e) { setMsg(String(e)) } finally { setBusy(false) }
  }

  async function loadServers() {
    const r = await getJSON<{ servers: Server[] }>('/plex/servers')
    setServers(r.servers)
    if (r.servers.length === 1) await chooseServer(r.servers[0].uri)
  }

  async function chooseServer(uri: string) {
    setUrl(uri)
    await save('plex.url', uri)
    await loadSections(uri)
  }

  async function loadSections(forURL: string) {
    try {
      const r = await getJSON<{ sections: Section[] }>(`/plex/sections?url=${encodeURIComponent(forURL)}`)
      setSections(r.sections)
    } catch (e) { setMsg('Couldn’t read libraries — ' + String(e)) }
  }

  const movieSecs = sections?.filter((s) => s.type === 'movie') ?? []
  const showSecs = sections?.filter((s) => s.type === 'show') ?? []

  return (
    <div style={{ marginBottom: 14 }}>
      <div className="field-row" style={{ marginBottom: 10 }}>
        <button className="btn btn-primary btn-sm" onClick={() => void signIn()} disabled={busy}>
          <Icon name="key" /> {configured ? 'Re-connect Plex' : 'Sign in with Plex'}
        </button>
        {configured && !servers && (
          <button className="btn btn-sm" onClick={() => void loadServers()} disabled={busy}>
            <Icon name="refresh" /> Load libraries
          </button>
        )}
        {msg && <span className="toast" style={{ fontSize: 12 }}>{msg}</span>}
      </div>

      {servers && servers.length > 0 && (
        <div className="field">
          <label>Server</label>
          <select className="input" value={url} onChange={(e) => void chooseServer(e.target.value)}>
            <option value="" disabled>Choose a server…</option>
            {servers.map((s) => <option key={s.uri} value={s.uri}>{s.name} — {s.uri}</option>)}
          </select>
        </div>
      )}

      {sections && (
        <>
          <LibrarySelect label="Movies library" secs={movieSecs} value={effective['plex.movie_section']}
            onPick={(k) => void save('plex.movie_section', k)} />
          <LibrarySelect label="Series library" secs={showSecs} value={effective['plex.tv_section']}
            onPick={(k) => void save('plex.tv_section', k)} />
          <LibrarySelect label="Anime library" secs={showSecs} value={effective['plex.anime_section']}
            onPick={(k) => void save('plex.anime_section', k)} />
        </>
      )}
    </div>
  )
}

function LibrarySelect({ label, secs, value, onPick }: {
  label: string; secs: Section[]; value?: string; onPick: (key: string) => void
}) {
  return (
    <div className="field">
      <label>{label}</label>
      <select className="input" value={value ?? ''} onChange={(e) => onPick(e.target.value)}>
        <option value="">— none —</option>
        {secs.map((s) => <option key={s.key} value={s.key}>{s.title} (#{s.key})</option>)}
      </select>
    </div>
  )
}

async function pollPin(id: number): Promise<boolean> {
  for (let i = 0; i < 48; i++) { // ~2 minutes at 2.5s
    await new Promise((r) => setTimeout(r, 2500))
    try {
      const r = await getJSON<{ authenticated: boolean }>(`/plex/pin/${id}`)
      if (r.authenticated) return true
    } catch { /* keep polling */ }
  }
  return false
}
