/** Derives a short uppercase file-type abbreviation from a filename, e.g. "latency.png" -> "PNG". */
export function fileTypeAbbrev(name: string): string {
  const ext = name.split('.').pop() ?? ''
  if (!ext || ext === name) return 'FILE'
  return ext.slice(0, 4).toUpperCase()
}
