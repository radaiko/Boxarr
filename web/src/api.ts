// Thin typed client over Boxarr's /api/v1 surface (04-internal-api.md).
const base = '/api/v1'

export async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(base + path)
  if (!r.ok) throw new Error(`GET ${path}: ${r.status}`)
  return (await r.json()) as T
}

export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  return sendJSON<T>('PUT', path, body)
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  return sendJSON<T>('POST', path, body)
}

export async function del(path: string): Promise<void> {
  const r = await fetch(base + path, { method: 'DELETE' })
  if (!r.ok) throw new Error(`DELETE ${path}: ${r.status}`)
}

async function sendJSON<T>(method: string, path: string, body: unknown): Promise<T> {
  const r = await fetch(base + path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status}`)
  return (await r.json()) as T
}

export interface SettingsResponse {
  settings: Record<string, string>
  configured: Record<string, boolean>
}

export interface Movie {
  id: number
  tmdbId: number
  title: string
  year: number
  monitored: boolean
  status: string
  hasFile: boolean
  posterPath?: string
  overview?: string
  libraryPath?: string
}

export interface MovieCandidate {
  tmdbId: number
  title: string
  year: number
  overview?: string
  posterPath?: string
  inLibrary: boolean
  libraryId?: number
}

export interface Release {
  releaseId: string
  title: string
  indexer: string
  protocol: string
  size: number
  quality?: string
  seeders?: number
  grabs?: number
  cached: boolean | null
  score: number
  rejected: boolean
  hasMagnet: boolean
}

export interface ListResponse<T> {
  items: T[]
  total: number
}

// TMDB image base + poster size, fetched once from settings; "" until loaded.
let posterBase = ''
export async function loadImageBase(): Promise<void> {
  try {
    const s = await getJSON<{ tmdb?: { imageBase?: string } }>('/settings')
    // settings shape varies; fall back to the canonical TMDB CDN.
    posterBase = (s.tmdb?.imageBase || 'https://image.tmdb.org/t/p/') + 'w342'
  } catch {
    posterBase = 'https://image.tmdb.org/t/p/w342'
  }
}
export function posterURL(path?: string): string {
  if (!path) return ''
  if (!posterBase) posterBase = 'https://image.tmdb.org/t/p/w342'
  return posterBase + path
}
