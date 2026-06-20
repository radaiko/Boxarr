import { useEffect, useState } from 'react'
import { getJSON } from '../api'

interface StorageResp {
  usedBytes: number
  downloads?: { active?: number }
  plan?: { tier: number; tierName: string; concurrentSlots: number; isSubscribed: boolean }
  usage?: { monthlyDownloadedBytes: number; cooldownUntil: string; inCooldown: boolean }
}

function gb(n: number): string {
  return (n / 1e9).toFixed(2) + ' GB'
}

export function Storage() {
  const [s, setS] = useState<StorageResp | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    getJSON<StorageResp>('/storage').then(setS).catch((e: unknown) => setErr(String(e)))
  }, [])

  if (err) return <p>Error: {err}</p>
  if (!s) return <p>Loading…</p>

  return (
    <section>
      <h2>Storage</h2>
      <p>Used on TorBox: <strong>{gb(s.usedBytes)}</strong></p>
      <p>Active downloads: {s.downloads?.active ?? 0}</p>
      {s.plan && (
        <p>
          Plan: <strong>{s.plan.tierName}</strong> ({s.plan.concurrentSlots} slots
          {s.plan.isSubscribed ? ', subscribed' : ''})
        </p>
      )}
      {s.usage && (
        <p>
          Monthly downloaded: {gb(s.usage.monthlyDownloadedBytes)}
          {s.usage.inCooldown ? ` · in cooldown until ${s.usage.cooldownUntil}` : ''}
        </p>
      )}
    </section>
  )
}
