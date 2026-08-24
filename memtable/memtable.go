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
	return e.Key < than.(*Entry).Key
}

func NewMemtable() Memtable {
	return Memtable{
		tree: btree.New(32),
	}
}

func (m *Memtable) Get(key string) (string, bool) {
	// todo: we would have to rely on prefix scan now
	// return the latest version
	// m.PrefixScan()
	item := m.tree.Get(&Entry{Key: key})
	if item == nil {
		return "", false
	}
	return item.(*Entry).Value, true
}

// Iterate loops through each of the key, value pair in the memTable
func (m *Memtable) Iterate(fn func(key, value string)) {
	m.tree.Ascend(func(item btree.Item) bool {
		e := item.(*Entry)
		fn(e.Key, e.Value)
		return true
	})
}

func (m *Memtable) Put(key, value string, txnId uint64) {
	entry := Entry{
		Key:   key,
		Value: value,
		TxnId: txnId,
	}
	if old := m.tree.ReplaceOrInsert(&entry); old != nil {
		m.size += (len(value))
		m.size -= (len(old.(*Entry).Value))
	} else {
		m.size += (len(key) + len(value))
	}
}

// given the prefix key, PrefixScan returns the serialised key
// and value in a map for all keys which match that prefix in the memtable.
func (m *Memtable) PrefixScan(prefixKey string) map[string]string {
	tableMap := map[string]string{}
	m.tree.AscendGreaterOrEqual(&Entry{Key: prefixKey}, func(item btree.Item) bool {
		e := item.(*Entry)
		key := e.Key
		if !strings.HasPrefix(key, prefixKey) {
			return false
		}
		tableMap[key] = e.Value
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
