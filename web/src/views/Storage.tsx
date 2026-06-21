import { useEffect, useState } from 'react'
import { getJSON, putJSON } from '../api'
import { Icon, Loading, ErrorBanner, gb, ago } from '../ui'

interface LimitEvent { kind: string; detail: string; createdAt: string }
interface StorageResp {
  usedBytes: number
  byCategory?: Record<string, number>
  downloads?: { active?: number; queued?: number; seeding?: number }
  plan?: { tier: number; tierName: string; concurrentSlots: number; isSubscribed: boolean }
  usage?: { monthlyDownloadedBytes: number; cooldownUntil: string; inCooldown: boolean }
  limits?: { dailyCap: number; usedToday: number; cooldownUntil: string; events: LimitEvent[] }
}

export function Storage() {
  const [s, setS] = useState<StorageResp | null>(null)
  const [err, setErr] = useState('')

  function load() { getJSON<StorageResp>('/storage').then(setS).catch((e: unknown) => setErr(String(e))) }
  useEffect(load, [])

  async function resetCap() {
    await putJSON('/settings', { settings: { 'torbox.daily_cap': '0', 'torbox.cooldown_until': '' } })
    load()
  }

  if (err) return <ErrorBanner message={err} />
  if (!s) return <Loading />

  const cats = Object.entries(s.byCategory || {}).filter(([, v]) => v > 0)
  const lim = s.limits
  const cooldownUntil = lim?.cooldownUntil || (s.usage?.inCooldown ? s.usage.cooldownUntil : '')

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
          <div className="sub">
            {s.downloads?.queued ? `${s.downloads.queued} queued` : 'none queued'}
            {s.downloads?.seeding ? ` · ${s.downloads.seeding} seeding` : ''}
          </div>
        </div>
        <div className="stat">
          <div className="label">Grabs today</div>
          <div className="value">{lim?.usedToday ?? 0}{lim && lim.dailyCap > 0 ? <small> / {lim.dailyCap} cap</small> : null}</div>
          <div className="sub">{lim && lim.dailyCap > 0 ? 'learned daily ceiling' : 'no cap learned yet'}</div>
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
            <div className="label">Bandwidth this month</div>
            <div className="value">{gb(s.usage.monthlyDownloadedBytes)}</div>
            <div className="sub">data TorBox served to you</div>
          </div>
        )}
      </div>

      {!s.plan && (
        <p className="muted" style={{ marginTop: 16 }}>
          Connect TorBox in Settings to see plan tier, slot limits, and usage.
        </p>
      )}

      {lim && (cooldownUntil || lim.dailyCap > 0 || lim.events.length > 0) && (
        <div style={{ marginTop: 22 }}>
          <div className="season-head">
            <Icon name="storage" /><h3>TorBox limits</h3>
            {(lim.dailyCap > 0 || cooldownUntil) && (
              <button className="btn btn-sm" style={{ marginLeft: 'auto' }} onClick={() => void resetCap()}>
                <Icon name="refresh" /> Reset learned limits
              </button>
            )}
          </div>
          {cooldownUntil && (
            <p className="muted" style={{ marginBottom: 10 }}>
              Submissions paused (TorBox cooldown) until <code>{cooldownUntil}</code>. Downloads already in progress continue.
            </p>
          )}
          {lim.events.length === 0 ? (
            <p className="muted">No rate-limit events recorded — Boxarr learns your ceiling from TorBox throttling.</p>
          ) : (
            <div className="table-wrap">
              <table className="tbl">
                <thead><tr><th style={{ width: 110 }}>Kind</th><th>Detail</th><th style={{ width: 120 }}>When</th></tr></thead>
                <tbody>
                  {lim.events.map((e, i) => (
                    <tr key={i}>
                      <td className="muted">{e.kind}</td>
                      <td className="rel-title">{e.detail}</td>
                      <td className="muted" style={{ fontSize: 12 }}>{ago(e.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
