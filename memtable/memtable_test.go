package memtable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// visible: only read transactions started before current txn and non-active + current txn
// within the visible ones, return the latest one
func TestGetReturnsVisibleVersion(t *testing.T) {
	mem := NewMemtable()
	mem.Put("name", "Gagan", 1)
	mem.Put("city", "Delhi", 2)
	mem.Put("city", "Bengaluru", 1)
	mem.Put("name", "Akash", 3)
	mem.Put("name", "Ansh", 5)
	mem.Put("city", "Mumbai", 6)

	testCases := []struct {
		txnId             uint64
		activeTxnMap      map[uint64]struct{}
		expectedName      string
		expectedCity      string
		expectedFoundName bool
		expectedFoundCity bool
	}{
		{
			txnId:        0,
			activeTxnMap: nil,
		},
		{
			txnId:        0,
			activeTxnMap: map[uint64]struct{}{0: struct{}{}},
		},
		{
			txnId:             1,
			activeTxnMap:      map[uint64]struct{}{1: struct{}{}},
			expectedName:      "Gagan",
			expectedCity:      "Bengaluru",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId: 2,
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				2: struct{}{},
			},
			expectedName:      "",
			expectedCity:      "Delhi",
			expectedFoundName: false,
			expectedFoundCity: true,
		},
		{
			txnId: 3,
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				2: struct{}{},
				3: struct{}{},
			},
			expectedName:      "Akash",
			expectedCity:      "",
			expectedFoundName: true,
			expectedFoundCity: false,
		},
		{
			txnId: 3,
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				3: struct{}{},
			},
			expectedName:      "Akash",
			expectedCity:      "Delhi",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId: 4,
			activeTxnMap: map[uint64]struct{}{
				2: struct{}{},
				3: struct{}{},
			},
			expectedName:      "Gagan",
			expectedCity:      "Bengaluru",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId: 5,
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				2: struct{}{},
				3: struct{}{},
				4: struct{}{},
				5: struct{}{},
				6: struct{}{},
			},
			expectedName:      "Ansh",
			expectedCity:      "",
			expectedFoundName: true,
			expectedFoundCity: false,
		},
		{
			txnId: 6,
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				2: struct{}{},
				3: struct{}{},
				6: struct{}{},
			},
			expectedName:      "Ansh",
			expectedCity:      "Mumbai",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
	}

	for _, tt := range testCases {
		name, foundName := mem.Get("name", tt.txnId, tt.activeTxnMap)
		city, foundCity := mem.Get("city", tt.txnId, tt.activeTxnMap)
		assert.Equal(t, tt.expectedName, name)
		assert.Equal(t, tt.expectedCity, city)
		assert.Equal(t, tt.expectedFoundName, foundName)
		assert.Equal(t, tt.expectedFoundCity, foundCity)
	}
}

func TestPrefixScanReturnsLatestVersions(t *testing.T) {
	mem := NewMemtable()
	mem.Put("users:1", "Alice", 1)
	mem.Put("users:2", "Bob", 2)
	mem.Put("users:1", "Alice2", 3)

	testCases := []struct {
		txnId        uint64
		activeTxnMap map[uint64]struct{}
		expectedMap  map[string]string
	}{
		{
			txnId: 1,
			expectedMap: map[string]string{
				"users:1": "Alice",
			},
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
			},
		},
		{
			txnId: 2,
			expectedMap: map[string]string{
				"users:2": "Bob",
			},
			activeTxnMap: map[uint64]struct{}{
				1: struct{}{},
				2: struct{}{},
			},
		},
		{
			txnId: 2,
			expectedMap: map[string]string{
				"users:1": "Alice",
				"users:2": "Bob",
			},
			activeTxnMap: map[uint64]struct{}{
				2: struct{}{},
			},
		},
		{
			txnId: 3,
			expectedMap: map[string]string{
				"users:1": "Alice2",
			},
			activeTxnMap: map[uint64]struct{}{
				2: struct{}{},
				3: struct{}{},
			},
		},
		{
			txnId: 3,
			expectedMap: map[string]string{
				"users:1": "Alice2",
				"users:2": "Bob",
			},
			activeTxnMap: map[uint64]struct{}{
				3: struct{}{},
			},
		},
	}

	for _, tt := range testCases {
		result := mem.PrefixScan("users:", tt.txnId, tt.activeTxnMap)
		assert.Equal(t, tt.expectedMap, result)
	}
}

func TestGetNonExistentKey(t *testing.T) {
	mem := NewMemtable()
	mem.Put("name", "Gagan", 1)
	_, found := mem.Get("age", 1, nil)
	assert.False(t, found)
}
