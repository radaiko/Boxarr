import { Storage } from './Storage'
import { WebDAV } from './WebDAV'

// TorBox combines the account/storage stats and the WebDAV mount browser into one
// view. onOpenCatalog lets a tracked item jump to its library page.
export function TorBox({ onOpenCatalog }: { onOpenCatalog?: (kind: string, id: number) => void }) {
  return (
    <>
      <Storage />
      <div style={{ height: 22 }} />
      <WebDAV onOpenCatalog={onOpenCatalog} />
    </>
  )
}
