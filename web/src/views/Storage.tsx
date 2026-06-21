import { useEffect, useState } from 'react'
import { getJSON } from '../api'
import { Loading, ErrorBanner, gb } from '../ui'

interface StorageResp {
  usedBytes: number
  byCategory?: Record<string, number>
  downloads?: { active?: number }
  plan?: { tier: number; tierName: string; concurrentSlots: number; isSubscribed: boolean }
  usage?: { monthlyDownloadedBytes: number; cooldownUntil: string; inCooldown: boolean }
}

export function Storage() {
  const [s, setS] = useState<StorageResp | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    getJSON<StorageResp>('/storage').then(setS).catch((e: unknown) => setErr(String(e)))
  }, [])

  if (err) return <ErrorBanner message={err} />
  if (!s) return <Loading />

  const cats = Object.entries(s.byCategory || {}).filter(([, v]) => v > 0)

  return (
    <section>
      <div className="stat-grid">
        <div className="stat">
          <div className="label">On TorBox</div>
          <div className="value">{gb(s.usedBytes)}</div>
          {cats.length > 0 && <div className="sub">{cats.map(([k, v]) => `${k} ${gb(v)}`).join(' · ')}</div>}
        </div>
        <div className="stat">
          <div className="label">Active downloads</div>
          <div className="value">{s.downloads?.active ?? 0}{s.plan ? <small> / {s.plan.concurrentSlots} slots</small> : null}</div>
        </div>
        {s.plan && (
          <div className="stat">
            <div className="label">Plan</div>
            <div className="value">{s.plan.tierName}</div>
            <div className="sub">{s.plan.isSubscribed ? 'subscribed' : 'not subscribed'}</div>
          </div>
        )}
        {s.usage && (
          <div className="stat">
            <div className="label">Downloaded this month</div>
            <div className="value">{gb(s.usage.monthlyDownloadedBytes)}</div>
            {s.usage.inCooldown && <div className="sub" style={{ color: 'var(--danger)' }}>in cooldown until {s.usage.cooldownUntil}</div>}
          </div>
        )}
      </div>
      {!s.plan && (
        <p className="muted" style={{ marginTop: 16 }}>
          Connect TorBox in Settings to see plan tier, slot limits, and monthly usage.
        </p>
      )}
    </section>
  )
}
