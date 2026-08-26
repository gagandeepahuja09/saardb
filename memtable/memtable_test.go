package memtable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemtablePutAndGet(t *testing.T) {
	mem := NewMemtable()

	mem.Put("name", "Gagan", 0)
	mem.Put("name", "Akshay", 1)
	mem.Put("name", "Akash", 2)

	value, found := mem.Get("name")
	assert.Equal(t, true, found)
	assert.Equal(t, "Akash", value)
}
