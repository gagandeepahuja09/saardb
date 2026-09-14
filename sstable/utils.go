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

func sortedSsTableEntries(keyValTxnIds []keyValueTxnId) []keyValueTxnId {
	sort.Slice(keyValTxnIds, func(i, j int) bool {
		if keyValTxnIds[i].key == keyValTxnIds[j].key {
			return keyValTxnIds[i].value < keyValTxnIds[j].value
		}
		return keyValTxnIds[i].key < keyValTxnIds[j].key
	})
	return keyValTxnIds
}
