import { useEffect, useState } from 'react'
import { getJSON, putJSON, type SettingsResponse } from '../api'

export function Settings() {
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    getJSON<SettingsResponse>('/settings')
      .then(setData)
      .catch((e: unknown) => setErr(String(e)))
  }, [])

  if (err) return <p>Error: {err}</p>
  if (!data) return <p>Loading…</p>

  return (
    <section>
      <h2>Settings</h2>
      <h3>Connections</h3>
      <ul>
        {Object.entries(data.configured).map(([k, v]) => (
          <li key={k}>
            {k}: {v ? 'configured' : 'not configured'}
          </li>
        ))}
      </ul>
      <h3>Overrides</h3>
      <SettingEditor data={data} onSaved={setData} />
    </section>
  )
}

function SettingEditor({
  data,
  onSaved,
}: {
  data: SettingsResponse
  onSaved: (d: SettingsResponse) => void
}) {
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')

  async function save() {
    const updated = await putJSON<SettingsResponse>('/settings', { settings: { [key]: value } })
    onSaved(updated)
    setKey('')
    setValue('')
  }

  return (
    <div>
      <input placeholder="key (e.g. prowlarr.url)" value={key} onChange={(e) => setKey(e.target.value)} />
      <input placeholder="value" value={value} onChange={(e) => setValue(e.target.value)} />
      <button onClick={() => void save()} disabled={!key}>
        Save
      </button>
      <pre>{JSON.stringify(data.settings, null, 2)}</pre>
    </div>
  )
}
