// Thin typed client over Boxarr's /api/v1 surface (04-internal-api.md).
const base = '/api/v1'

export async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(base + path)
  if (!r.ok) throw new Error(`GET ${path}: ${r.status}`)
  return (await r.json()) as T
}

export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(base + path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`PUT ${path}: ${r.status}`)
  return (await r.json()) as T
}

export interface SettingsResponse {
  settings: Record<string, string>
  configured: Record<string, boolean>
}
