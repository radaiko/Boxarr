import { useEffect, useState } from 'react'

export type ToastKind = 'ok' | 'err' | 'info'
interface Toast { id: number; msg: string; kind: ToastKind }

let seq = 0
let toasts: Toast[] = []
const listeners = new Set<(t: Toast[]) => void>()
const emit = () => listeners.forEach((l) => l(toasts))

// toast shows a transient message. Call from anywhere (action handlers, catch
// blocks) for consistent feedback.
export function toast(msg: string, kind: ToastKind = 'info') {
  const t: Toast = { id: ++seq, msg, kind }
  toasts = [...toasts, t]
  emit()
  setTimeout(() => {
    toasts = toasts.filter((x) => x.id !== t.id)
    emit()
  }, kind === 'err' ? 6000 : 3500)
}

// Toaster renders the live toast stack; mount once near the app root.
export function Toaster() {
  const [items, setItems] = useState<Toast[]>(toasts)
  useEffect(() => {
    listeners.add(setItems)
    return () => {
      listeners.delete(setItems)
    }
  }, [])
  if (items.length === 0) return null
  return (
    <div className="toaster">
      {items.map((t) => (
        <div key={t.id} className={`toast-pop ${t.kind}`} onClick={() => { toasts = toasts.filter((x) => x.id !== t.id); emit() }}>
          {t.msg}
        </div>
      ))}
    </div>
  )
}
