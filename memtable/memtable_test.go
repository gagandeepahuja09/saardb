package memtable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetReturnsLatestVersion(t *testing.T) {
	mem := NewMemtable()
	mem.Put("name", "Gagan", 1)
	mem.Put("city", "Delhi", 2)
	mem.Put("city", "Bengaluru", 1)
	mem.Put("name", "Akash", 3)
	mem.Put("name", "Ansh", 5)
	mem.Put("city", "Mumbai", 6)

	testCases := []struct {
		txnId             uint64
		activeTxnIds      []uint64
		expectedName      string
		expectedCity      string
		expectedFoundName bool
		expectedFoundCity bool
	}{
		{
			txnId:        0,
			activeTxnIds: nil,
		},
		{
			txnId:        0,
			activeTxnIds: []uint64{0},
		},
		{
			txnId:             1,
			activeTxnIds:      []uint64{1},
			expectedName:      "Gagan",
			expectedCity:      "Bengaluru",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId:             2,
			activeTxnIds:      []uint64{1, 2},
			expectedName:      "",
			expectedCity:      "Delhi",
			expectedFoundName: false,
			expectedFoundCity: true,
		},
		{
			txnId:             3,
			activeTxnIds:      []uint64{1, 2, 3},
			expectedName:      "Akash",
			expectedCity:      "",
			expectedFoundName: true,
			expectedFoundCity: false,
		},
		{
			txnId:             3,
			activeTxnIds:      []uint64{1, 3},
			expectedName:      "Akash",
			expectedCity:      "Delhi",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId:             4,
			activeTxnIds:      []uint64{2, 3},
			expectedName:      "Gagan",
			expectedCity:      "Bengaluru",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
		{
			txnId:             5,
			activeTxnIds:      []uint64{1, 2, 3, 4, 5, 6},
			expectedName:      "Ansh",
			expectedCity:      "",
			expectedFoundName: true,
			expectedFoundCity: false,
		},
		{
			txnId:             6,
			activeTxnIds:      []uint64{1, 2, 3, 6},
			expectedName:      "Ansh",
			expectedCity:      "Mumbai",
			expectedFoundName: true,
			expectedFoundCity: true,
		},
	}

	for _, tt := range testCases {
		name, foundName := mem.Get("name", tt.txnId, tt.activeTxnIds)
		city, foundCity := mem.Get("city", tt.txnId, tt.activeTxnIds)
		assert.Equal(t, tt.expectedName, name)
		assert.Equal(t, tt.expectedCity, city)
		assert.Equal(t, tt.expectedFoundName, foundName)
		assert.Equal(t, tt.expectedFoundCity, foundCity)
	}

	// name, foundName = mem.Get("name", 1, nil)
	// city, foundCity = mem.Get("city", 1, nil)
	// assert.Equal(t, "", name)
	// assert.Equal(t, true, foundName)
	// assert.Equal(t, "", city)
	// assert.Equal(t, true, foundCity)
}

func TestPrefixScanReturnsLatestVersions(t *testing.T) {
	mem := NewMemtable()
	mem.Put("users:1", "Alice", 1)
	mem.Put("users:2", "Bob", 2)
	mem.Put("users:1", "Alice2", 3)

	result := mem.PrefixScan("users:")
	assert.Equal(t, "Alice2", result["users:1"])
	assert.Equal(t, "Bob", result["users:2"])
}

func TestGetNonExistentKey(t *testing.T) {
	mem := NewMemtable()
	mem.Put("name", "Gagan", 1)
	_, found := mem.Get("age", 1, nil)
	assert.False(t, found)
}
