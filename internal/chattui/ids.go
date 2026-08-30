package chattui

import "strconv"

// parseID and formatID convert between the wire's string ids and the int64 TagMap uses
// internally — every id on the wire is a string (SPEC.md §4, "IDs serialize as strings").
func parseID(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
func formatID(id int64) string        { return strconv.FormatInt(id, 10) }
