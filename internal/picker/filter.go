package picker

import "strings"

// matches reports whether a row's searchable text contains the query as a
// subsequence — "f14" finds "factory-14-search".
//
// The list is short by construction (reception, a gaffer or two, the workers
// they dispatched), so this filters without ranking: rows keep their
// most-recently-active order, which is more useful than a relevance score when
// there are eight rows on screen.
func matches(text, query string) bool {
	if query == "" {
		return true
	}
	text, query = strings.ToLower(text), strings.ToLower(query)
	// A literal substring is the common case and the cheapest.
	if strings.Contains(text, query) {
		return true
	}
	want := []rune(query)
	i := 0
	for _, r := range text {
		if want[i] == r {
			if i++; i == len(want) {
				return true
			}
		}
	}
	return false
}
