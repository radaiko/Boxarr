// Thin typed client over Boxarr's /api/v1 surface (04-internal-api.md).
const base = '/api/v1'

// When the instance has an API key set, the SPA stores it locally and sends it
// on every request as X-Api-Key. Empty = open instance.
const KEY_STORE = 'boxarr.apiKey'
let apiKey = (typeof localStorage !== 'undefined' && localStorage.getItem(KEY_STORE)) || ''

export function setApiKey(k: string): void {
  apiKey = k
  if (typeof localStorage === 'undefined') return
  if (k) localStorage.setItem(KEY_STORE, k)
  else localStorage.removeItem(KEY_STORE)
}
export function getApiKey(): string { return apiKey }

function headers(extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...extra }
  if (apiKey) h['X-Api-Key'] = apiKey
  return h
}

function fail(method: string, path: string, status: number): Error {
  if (status === 401) return new Error(`unauthorized (${status}) — set the correct API key in Settings`)
  return new Error(`${method} ${path}: ${status}`)
}

export async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(base + path, { headers: headers() })
  if (!r.ok) throw fail('GET', path, r.status)
  return (await r.json()) as T
}

export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  return sendJSON<T>('PUT', path, body)
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  return sendJSON<T>('POST', path, body)
}

export async function del(path: string): Promise<void> {
  const r = await fetch(base + path, { method: 'DELETE', headers: headers() })
  if (!r.ok) throw fail('DELETE', path, r.status)
}

async function sendJSON<T>(method: string, path: string, body: unknown): Promise<T> {
  const r = await fetch(base + path, {
    method,
    headers: headers({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(body),
  })
  if (!r.ok) throw fail(method, path, r.status)
  return (await r.json()) as T
}

export interface SettingsResponse {
  settings: Record<string, string>
  configured: Record<string, boolean>
}

// FileMeta describes the downloaded file behind a library item, parsed from the
// release name (resolution, source, codec, HDR/DV, audio, languages, group).
export interface FileMeta {
  name: string
  path?: string
  resolution?: string
  source?: string
  codec?: string
  dynamicRange?: string
  audio?: string
  languages?: string[]
  group?: string
  quality?: string
}

export interface Movie {
  id: number
  tmdbId: number
  title: string
  year: number
  monitored: boolean
  status: string
  hasFile: boolean
  langMissing?: boolean
  posterPath?: string
  overview?: string
  libraryPath?: string
  file?: FileMeta
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
  resolution?: string
  languages?: string[]
  subs?: boolean
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

export interface Series {
  id: number
  tmdbId: number
  tvdbId?: number
  title: string
  year: number
  monitored: boolean
  status: string
  seriesType?: string
  posterPath?: string
  overview?: string
  seasons?: Season[]
}

export interface Season {
  seasonNumber: number
  monitored: boolean
  status: string
  episodes?: Episode[]
}

export interface Episode {
  id: number
  seasonNumber: number
  episodeNumber: number
  title: string
  airDate?: string
  status: string
  monitored: boolean
  hasFile: boolean
  langMissing?: boolean
  file?: FileMeta
  lastSearched?: string
}

export interface SeriesCandidate {
  tmdbId: number
  tvdbId?: number
  title: string
  year: number
  overview?: string
  posterPath?: string
  inLibrary: boolean
}
