package memtable

import (
	"strings"

	"github.com/google/btree"
)

const (
	memtableSizeLimit = 1000 // 1 kb for testing (for now)
)

type Memtable struct {
	tree *btree.BTree
	size int
}

type Entry struct {
	Key   string
	Value string
	TxnId uint64
}

func (e *Entry) Less(than btree.Item) bool {
	if e.Key == than.(*Entry).Key {
		return e.TxnId < than.(*Entry).TxnId
	}
	return e.Key < than.(*Entry).Key
}

func NewMemtable() Memtable {
	return Memtable{
		tree: btree.New(32),
	}
}

func (m *Memtable) Get(key string, txnId uint64, activeTxnMap map[uint64]struct{}) (string, bool) {
	value := ""
	found := false
	m.tree.AscendGreaterOrEqual(&Entry{Key: key}, func(item btree.Item) bool {
		e := item.(*Entry)
		if e.Key != key {
			return false
		}
		_, isTxnActive := activeTxnMap[e.TxnId]
		// since txnIds are in ascending order, we pick the latest non-active one
		if (e.TxnId == txnId) || (e.TxnId < txnId && !isTxnActive) {
			value = e.Value
			found = true
		}
		return true
	})
	return value, found
}

// Iterate loops through each of the key, value pair in the memTable
func (m *Memtable) Iterate(fn func(key, value string, txnId uint64)) {
	m.tree.Ascend(func(item btree.Item) bool {
		e := item.(*Entry)
		fn(e.Key, e.Value, e.TxnId)
		return true
	})
}

func (m *Memtable) Put(key, value string, txnId uint64) {
	entry := Entry{
		Key:   key,
		Value: value,
		TxnId: txnId,
	}
	// todo: revisit memtable flush condition logic after some research
	if old := m.tree.ReplaceOrInsert(&entry); old != nil {
		m.size += (len(value))
		m.size -= (len(old.(*Entry).Value))
	} else {
		m.size += (len(key) + len(value))
	}
}

// given the prefix key, PrefixScan returns the serialised key
// and value in a map for all keys which match that prefix in the memtable.
func (m *Memtable) PrefixScan(prefixKey string, txnId uint64, activeTxnMap map[uint64]struct{}) map[string]string {
	tableMap := map[string]string{}
	m.tree.AscendGreaterOrEqual(&Entry{Key: prefixKey}, func(item btree.Item) bool {
		e := item.(*Entry)
		key := e.Key
		if !strings.HasPrefix(key, prefixKey) {
			return false
		}
		// since txnIds are in ascending order, we pick the latest non-active one for each key
		_, isTxnActive := activeTxnMap[e.TxnId]
		if e.TxnId == txnId || (e.TxnId < txnId && !isTxnActive) {
			tableMap[key] = e.Value
		}
		return true
	})
	return tableMap
}

func (m *Memtable) ShouldFlush() bool {
	return m.size >= memtableSizeLimit
}

func (m *Memtable) GetSize() int {
	return m.size
}

func (m *Memtable) Clear() {
	m.tree.Clear(false)
	m.size = 0
}
