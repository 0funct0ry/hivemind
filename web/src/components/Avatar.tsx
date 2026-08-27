/** Derives up to two initials from a display name/username, matching the mockup's `.av`. */
function initialsFor(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}

export function Avatar({
  name,
  color,
  size,
  className,
  title,
}: {
  name: string
  color: string
  size: number
  className?: string
  title?: string
}) {
  return (
    <span
      className={'grid shrink-0 place-items-center font-mono font-semibold text-white ' + (className ?? '')}
      style={{
        width: size,
        height: size,
        borderRadius: 5,
        backgroundColor: color,
        fontSize: Math.round(size * 0.4),
        letterSpacing: '-0.02em',
      }}
      title={title}
      aria-hidden
    >
      {initialsFor(name)}
    </span>
  )
}
