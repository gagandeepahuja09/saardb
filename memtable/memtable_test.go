package memtable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetReturnsLatestVersion(t *testing.T) {
	mem := NewMemtable()
	mem.Put("name", "Gagan", 1)
	mem.Put("city", "Delhi", 2)
	mem.Put("city", "Mumbai", 1)
	mem.Put("name", "Akash", 3)

	name, foundName := mem.Get("name")
	city, foundCity := mem.Get("city")
	assert.Equal(t, "Akash", name)
	assert.Equal(t, true, foundName)
	assert.Equal(t, "Delhi", city)
	assert.Equal(t, true, foundCity)
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
	_, found := mem.Get("age")
	assert.False(t, found)
}
