package sstable

import (
	"sort"
)

func sortedKeys(mp map[string]valueTxnId) (keys []string) {
	for key, _ := range mp {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
