package chatclient

import "strconv"

// TagMap assigns short, client-local base36 tags ("a0", "a1", ..., "az", "b0", ...) to message
// ids in receipt order, per SPEC.md §7.7. Tags are scoped to the current view (channel or open
// thread) and known only to the client — never sent to or known by the server.
type TagMap struct {
	byTag   map[string]int64
	byID    map[int64]string
	counter int64
}

// NewTagMap constructs an empty TagMap.
func NewTagMap() *TagMap {
	return &TagMap{byTag: make(map[string]int64), byID: make(map[int64]string)}
}

// tagOffset shifts the counter so the first assigned tag renders as "a0" (10*36+0 in base36)
// rather than "00" — matching SPEC.md §7.7's a0, a1, ..., az, b0, ... scheme. Once the tens
// place would roll past 'z' (36*36 tags assigned in one view), FormatInt naturally grows to a
// third base36 digit rather than wrapping, which is intentional: tags stay unique for the
// lifetime of the view instead of colliding with an already-rendered one.
const tagOffset = 10 * 36

// Assign assigns the next tag to messageID, or returns its existing tag if already assigned.
func (t *TagMap) Assign(messageID int64) string {
	if tag, ok := t.byID[messageID]; ok {
		return tag
	}
	tag := strconv.FormatInt(t.counter+tagOffset, 36)
	t.counter++
	t.byTag[tag] = messageID
	t.byID[messageID] = tag
	return tag
}

// Resolve returns the message id for a tag, if assigned.
func (t *TagMap) Resolve(tag string) (int64, bool) {
	id, ok := t.byTag[tag]
	return id, ok
}

// Keys returns every currently-assigned tag, in no particular order — used for .thread's tab
// completion.
func (t *TagMap) Keys() []string {
	keys := make([]string, 0, len(t.byTag))
	for k := range t.byTag {
		keys = append(keys, k)
	}
	return keys
}

// Reset clears every assignment and restarts the counter, called on .join, .thread, and .clear.
func (t *TagMap) Reset() {
	t.byTag = make(map[string]int64)
	t.byID = make(map[int64]string)
	t.counter = 0
}
