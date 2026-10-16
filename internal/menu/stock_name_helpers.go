package menu

import (
	"strings"
	"unicode"
)

func normalizeStockKey(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stockNameKeys(names ...string) []string {
	keys := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		key := normalizeStockKey(name)
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

func stockByNameLookup(by map[string]float64, names ...string) float64 {
	for _, candidate := range stockNameKeys(names...) {
		if value, ok := by[candidate]; ok {
			return value
		}
	}
	for key, value := range by {
		normalized := normalizeStockKey(key)
		for _, candidate := range stockNameKeys(names...) {
			if normalized == candidate {
				return value
			}
		}
	}
	return 0
}
