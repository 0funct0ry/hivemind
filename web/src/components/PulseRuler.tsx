import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type ActivityResponse } from '../lib/api'
import { rafThrottle, prefersReducedMotion } from '../lib/throttle'

const HEIGHT = 28
const GAP = 1.6
const BUCKETS = 48

// SVG presentation attributes can't use Tailwind utility classes, and the design tokens
// aren't exposed as CSS custom properties, so these mirror tailwind.config.ts directly.
const COLOR_TEAL = '#0E6E60'
const COLOR_TEAL_SOFT = '#DCEBE6'
const COLOR_POLLEN = '#D4930B'

function formatBucketLabel(startMs: number, count: number): string {
  const time = new Date(startMs).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
  return `${time} · ${count} message${count === 1 ? '' : 's'}`
}

export function PulseRuler({ channelId, onJump }: { channelId: string; onJump: (messageId: string) => void }) {
  const { data } = useQuery({
    queryKey: ['activity', channelId],
    queryFn: () => api.getActivity(channelId, { buckets: BUCKETS }),
    staleTime: 30_000,
  })

  const svgRef = useRef<SVGSVGElement>(null)
  const [hovered, setHovered] = useState<number | null>(null)
  const [tooltipX, setTooltipX] = useState(0)
  const [focused, setFocused] = useState(0)
  const [announce, setAnnounce] = useState('')
  const draggingRef = useRef(false)

  if (!data) {
    return <div className="h-[28px] border-b border-rule bg-paper" aria-hidden />
  }

  const n = data.counts.length
  const max = data.max
  const isEmpty = max === 0

  const bucketFromClientX = (clientX: number): number => {
    const svg = svgRef.current
    if (!svg) return 0
    const rect = svg.getBoundingClientRect()
    const x = clientX - rect.left
    const w = rect.width
    const bw = (w - (n - 1) * GAP) / n
    const i = Math.floor(x / (bw + GAP))
    return Math.max(0, Math.min(n - 1, i))
  }

  const jump = (i: number) => {
    setFocused(i)
    const bucketStart = data.from + i * data.bucket_ms
    setAnnounce(formatBucketLabel(bucketStart, data.counts[i] ?? 0))
    const msgId = data.bucket_message_ids[i]
    if (msgId) onJump(msgId)
  }

  const throttledDragMove = rafThrottle((clientX: number) => {
    const i = bucketFromClientX(clientX)
    setHovered(i)
    jump(i)
  })

  const handleMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const i = bucketFromClientX(e.clientX)
    setHovered(i)
    const rect = svgRef.current?.getBoundingClientRect()
    if (rect) setTooltipX(Math.min(Math.max(e.clientX - rect.left, 0), rect.width))
    const bucketStart = data.from + i * data.bucket_ms
    setAnnounce(formatBucketLabel(bucketStart, data.counts[i] ?? 0))
    if (draggingRef.current) throttledDragMove(e.clientX)
  }

  const handleMouseLeave = () => {
    setHovered(null)
  }

  const handleMouseDown = (e: React.MouseEvent<SVGSVGElement>) => {
    draggingRef.current = true
    const i = bucketFromClientX(e.clientX)
    jump(i)
    const onMove = (ev: MouseEvent) => throttledDragMove(ev.clientX)
    const onUp = () => {
      draggingRef.current = false
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  const handleKeyDown = (e: React.KeyboardEvent<SVGSVGElement>) => {
    if (e.key === 'ArrowLeft') {
      e.preventDefault()
      const i = Math.max(0, focused - 1)
      setFocused(i)
      const bucketStart = data.from + i * data.bucket_ms
      setAnnounce(formatBucketLabel(bucketStart, data.counts[i] ?? 0))
    } else if (e.key === 'ArrowRight') {
      e.preventDefault()
      const i = Math.min(n - 1, focused + 1)
      setFocused(i)
      const bucketStart = data.from + i * data.bucket_ms
      setAnnounce(formatBucketLabel(bucketStart, data.counts[i] ?? 0))
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      jump(focused)
    }
  }

  if (isEmpty) {
    return (
      <div className="relative flex h-[28px] items-center justify-center border-b border-rule bg-paper">
        <div className="absolute inset-x-4 top-1/2 h-px bg-rule" />
        <span className="relative z-10 bg-paper px-2 font-mono text-[10px] text-ink-3">No messages yet</span>
      </div>
    )
  }

  const w = 480 // viewBox coordinate space; scales to 100% width via preserveAspectRatio=none
  const bw = (w - (n - 1) * GAP) / n
  const reduced = prefersReducedMotion()

  return (
    <div className="border-b border-rule bg-paper px-2 py-1">
      <div className="relative">
        <svg
          ref={svgRef}
          role="slider"
          tabIndex={0}
          aria-label="Message activity timeline"
          aria-valuemin={0}
          aria-valuemax={n - 1}
          aria-valuenow={focused}
          viewBox={`0 0 ${w} ${HEIGHT}`}
          preserveAspectRatio="none"
          className="block h-[28px] w-full cursor-crosshair outline-none focus-visible:ring-2 focus-visible:ring-teal"
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          onMouseDown={handleMouseDown}
          onKeyDown={handleKeyDown}
          onClick={(e) => jump(bucketFromClientX(e.clientX))}
        >
          {data.counts.map((v, i) => {
            const h = v === 0 ? 0 : Math.max(2, (v / max) * (HEIGHT - 9))
            const active = hovered === i || focused === i
            return (
              <rect
                key={i}
                x={i * (bw + GAP)}
                y={HEIGHT - h}
                width={bw}
                height={h}
                fill={active ? COLOR_TEAL : COLOR_TEAL_SOFT}
                className={reduced ? '' : 'transition-colors duration-150'}
              />
            )
          })}
          {data.mentions.map((m) => (
            <rect
              key={`mention-${m.bucket}`}
              x={m.bucket * (bw + GAP) + bw / 2 - 1.1}
              y={0}
              width={2.2}
              height={4.5}
              fill={COLOR_POLLEN}
            />
          ))}
          {data.unread_boundary && (
            <line
              x1={data.unread_boundary.bucket * (bw + GAP)}
              x2={data.unread_boundary.bucket * (bw + GAP)}
              y1={0}
              y2={HEIGHT}
              stroke={COLOR_POLLEN}
              strokeWidth={1.5}
            />
          )}
        </svg>
        {hovered !== null && (
          <div
            className="pointer-events-none absolute top-0 z-10 -translate-x-1/2 -translate-y-full whitespace-nowrap rounded bg-deep px-[7px] py-[3px] font-mono text-[9.5px] text-paper"
            style={{ left: tooltipX }}
          >
            {formatBucketLabel(data.from + hovered * data.bucket_ms, data.counts[hovered] ?? 0)}
          </div>
        )}
      </div>
      <div aria-live="polite" className="sr-only">
        {announce}
      </div>
    </div>
  )
}

/** Bumps the last bucket's count locally after a new message, without a refetch. */
export function bumpActivityBucket(
  queryClient: ReturnType<typeof useQueryClient>,
  channelId: string,
): void {
  queryClient.setQueryData<ActivityResponse>(['activity', channelId], (old) => {
    if (!old || old.counts.length === 0) return old
    const counts = [...old.counts]
    const last = counts.length - 1
    counts[last] += 1
    return { ...old, counts, max: Math.max(old.max, counts[last]) }
  })
}
