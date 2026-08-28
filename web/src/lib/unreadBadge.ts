import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from './api'

const FAVICON_SIZE = 32
const ORIGINAL_HREF = '/favicon.svg'
const ORIGINAL_TYPE = 'image/svg+xml'

function getFaviconLink(): HTMLLinkElement | null {
  return document.querySelector('link[rel="icon"]')
}

function drawBadge(count: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => {
      const canvas = document.createElement('canvas')
      canvas.width = FAVICON_SIZE
      canvas.height = FAVICON_SIZE
      const ctx = canvas.getContext('2d')
      if (!ctx) {
        reject(new Error('no 2d context'))
        return
      }
      ctx.drawImage(img, 0, 0, FAVICON_SIZE, FAVICON_SIZE)

      const r = FAVICON_SIZE * 0.28
      const cx = FAVICON_SIZE - r - 1
      const cy = r + 1
      ctx.beginPath()
      ctx.arc(cx, cy, r, 0, Math.PI * 2)
      ctx.fillStyle = '#C9860A'
      ctx.fill()

      if (count > 0) {
        ctx.fillStyle = '#FFFFFF'
        ctx.font = `bold ${Math.round(r * 1.15)}px sans-serif`
        ctx.textAlign = 'center'
        ctx.textBaseline = 'middle'
        ctx.fillText(count > 9 ? '9+' : String(count), cx, cy + 1)
      }

      resolve(canvas.toDataURL('image/png'))
    }
    img.onerror = () => reject(new Error('failed to load favicon'))
    img.src = ORIGINAL_HREF
  })
}

function restoreFavicon() {
  const link = getFaviconLink()
  if (!link) return
  link.type = ORIGINAL_TYPE
  link.href = ORIGINAL_HREF
}

async function applyBadge(count: number) {
  const link = getFaviconLink()
  if (!link) return
  try {
    const dataUrl = await drawBadge(count)
    link.type = 'image/png'
    link.href = dataUrl
  } catch {
    // Rasterizing failed (e.g. canvas tainted); leave the plain favicon alone.
  }
}

/**
 * Badges the favicon and prefixes the document title with "(N) " while the tab is hidden
 * and there is unread activity, clearing both on refocus. Purely client-side, driven by the
 * existing /unreads poll and the WS events that already invalidate it — no new endpoint.
 */
export function useUnreadBadge() {
  const { data } = useQuery({ queryKey: ['unreads'], queryFn: api.unreadSummary })
  const baseTitleRef = useRef(document.title.replace(/^\(\d+\+?\)\s*/, ''))

  useEffect(() => {
    function apply() {
      const total = data?.total_unread ?? 0
      if (total > 0 && document.visibilityState !== 'visible') {
        document.title = `(${total > 99 ? '99+' : total}) ${baseTitleRef.current}`
        void applyBadge(total)
      } else {
        document.title = baseTitleRef.current
        restoreFavicon()
      }
    }

    apply()
    document.addEventListener('visibilitychange', apply)
    return () => document.removeEventListener('visibilitychange', apply)
  }, [data])
}
