package mentions

import (
	"regexp"
	"strings"
)

// Ref represents a parsed mention reference.
type Ref struct {
	Kind  string // "user" | "channel" | "here"
	Name  string // username, "channel", or "here"
	Start int    // byte start offset including '@'
	End   int    // byte end offset (exclusive)
}

var mentionRegex = regexp.MustCompile(`@([a-zA-Z0-9][a-zA-Z0-9._-]{1,31})`)

// Mask replaces fenced code blocks, inline code spans, markdown link URLs, and raw URLs
// with spaces to prevent mentions parsing inside them, while preserving character byte offsets.
func Mask(body string) string {
	buf := []byte(body)
	n := len(buf)

	i := 0
	for i < n {
		// Check for fenced code block: ```
		if i+2 < n && buf[i] == '`' && buf[i+1] == '`' && buf[i+2] == '`' {
			buf[i] = ' '
			buf[i+1] = ' '
			buf[i+2] = ' '
			i += 3
			found := false
			for i < n {
				if i+2 < n && buf[i] == '`' && buf[i+1] == '`' && buf[i+2] == '`' {
					buf[i] = ' '
					buf[i+1] = ' '
					buf[i+2] = ' '
					i += 3
					found = true
					break
				}
				if buf[i] == '\n' || buf[i] == '\r' {
					i++
					continue
				}
				buf[i] = ' '
				i++
			}
			if !found {
				for i < n {
					if buf[i] == '\n' || buf[i] == '\r' {
						i++
						continue
					}
					buf[i] = ' '
					i++
				}
			}
			continue
		}

		// Check for inline code span: `
		if buf[i] == '`' {
			buf[i] = ' '
			i++
			for i < n {
				if buf[i] == '`' {
					buf[i] = ' '
					i++
					break
				}
				if buf[i] == '\n' || buf[i] == '\r' {
					i++
					continue
				}
				buf[i] = ' '
				i++
			}
			continue
		}

		// Check for markdown link: [text](url)
		if buf[i] == '[' {
			bracketCount := 1
			j := i + 1
			for j < n {
				if buf[j] == '[' {
					bracketCount++
				} else if buf[j] == ']' {
					bracketCount--
					if bracketCount == 0 {
						break
					}
				}
				j++
			}
			if j < n && buf[j] == ']' && j+1 < n && buf[j+1] == '(' {
				parenCount := 1
				k := j + 2
				for k < n {
					if buf[k] == '(' {
						parenCount++
					} else if buf[k] == ')' {
						parenCount--
						if parenCount == 0 {
							break
						}
					}
					k++
				}
				if k < n && buf[k] == ')' {
					// Mask everything from '(' to ')'
					for m := j + 1; m <= k; m++ {
						buf[m] = ' '
					}
					i = k + 1
					continue
				}
			}
		}

		// Check for raw URL: http:// or https:// or ftp://
		isURL := false
		urlPrefixes := []string{"http://", "https://", "ftp://"}
		for _, prefix := range urlPrefixes {
			if i+len(prefix) <= n && string(buf[i:i+len(prefix)]) == prefix {
				isURL = true
				break
			}
		}
		if isURL {
			for i < n && buf[i] != ' ' && buf[i] != '\t' && buf[i] != '\n' && buf[i] != '\r' {
				buf[i] = ' '
				i++
			}
			continue
		}

		i++
	}

	return string(buf)
}

// Parse extracts valid mention references from the message body.
func Parse(body string) []Ref {
	masked := Mask(body)
	locs := mentionRegex.FindAllStringSubmatchIndex(masked, -1)

	var refs []Ref
	for _, loc := range locs {
		start := loc[0]
		end := loc[1]

		// Must not match an @ preceded by a word character
		if start > 0 {
			prev := body[start-1]
			isWordChar := (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_'
			if isWordChar {
				continue
			}
		}

		// Trim trailing punctuation (dot, underscore, dash) from username
		nameStart := loc[2]
		nameEnd := loc[3]
		name := body[nameStart:nameEnd]
		for len(name) > 0 {
			last := name[len(name)-1]
			if last == '.' || last == '_' || last == '-' {
				name = name[:len(name)-1]
				nameEnd--
				end--
			} else {
				break
			}
		}

		// Validate length and starting character rules for username (except for special here/channel names)
		lowerName := strings.ToLower(name)
		if lowerName == "here" || lowerName == "channel" {
			kind := lowerName
			refs = append(refs, Ref{
				Kind:  kind,
				Name:  lowerName,
				Start: start,
				End:   end,
			})
			continue
		}

		if len(name) < 2 || len(name) > 32 {
			continue
		}

		first := name[0]
		firstOK := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')
		if !firstOK {
			continue
		}

		refs = append(refs, Ref{
			Kind:  "user",
			Name:  name,
			Start: start,
			End:   end,
		})
	}

	return refs
}
