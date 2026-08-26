/** Calls `fn` at most once per `ms`; the first call in a window fires immediately. */
export function throttle<A extends unknown[]>(fn: (...args: A) => void, ms: number): (...args: A) => void {
  let last = 0
  return (...args: A) => {
    const now = Date.now()
    if (now - last >= ms) {
      last = now
      fn(...args)
    }
  }
}

/** Coalesces rapid calls to at most one per animation frame; the last call in a frame wins. */
export function rafThrottle<A extends unknown[]>(fn: (...args: A) => void): (...args: A) => void {
  let scheduled = false
  let lastArgs: A
  return (...args: A) => {
    lastArgs = args
    if (scheduled) return
    scheduled = true
    requestAnimationFrame(() => {
      scheduled = false
      fn(...lastArgs)
    })
  }
}

export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
}
