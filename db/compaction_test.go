package db

import (
	"math"
	"os"
	"testing"

	"github.com/golang-db/sstable"
	"github.com/stretchr/testify/assert"
)

var testDbConfig = Config{
	SsTableConfig: sstable.Config{
		DataFilesDirectory: "temp",
	},
}

func dbDirCleanUp(t *testing.T) {
	err := os.RemoveAll("temp")
	os.Remove("wal.log")
	assert.NoError(t, err)
}

// start explicit transaction after every write to two different keys.
// trigger explicit compaction with txnId = 8
// this would mean that for name key, we will only keep the latest version
// less than or equal to txnId = 8 which is v1
// and delete the version  v2. similarly for name 2: v5 is kept and v6 is deleted.
// hence, when txn3 tries to read, it gets empty result as txnId = 2 was deleted
// from sstable during compaction.
// few values are intentionally kept in descending order to assert for sorting by key
// and then by txnId.
func TestCompactionCleansUpVersionsBelowMinActive(t *testing.T) {
	defer dbDirCleanUp(t)
	db, err := NewDB(testDbConfig)
	assert.NoError(t, err)

	db.Put("name", "v2")    // txnId = 2
	txn3, err := db.Begin() // txnId = 3
	db.Put("name2", "v6")   // txnId = 4
	txn5, err := db.Begin() // txnId = 5
	txn5.Put("name3", "here")
	db.Put("name", "v1")     // txnId = 6
	txn7, err := db.Begin()  // txnId = 7
	db.Put("name2", "v5")    // txnId = 8
	txn9, err := db.Begin()  // txnId = 9
	db.Put("name", "v3")     // txnId = 10
	txn11, err := db.Begin() // txnId = 11
	db.Put("name2", "v4")    // txnId = 12
	txn13, err := db.Begin() // txnId = 13

	db.FlushMemtable()
	db.ssTable.RunCompaction(8)

	res, err := txn3.Get("name")
	assert.Equal(t, "", res)

	res, err = txn5.Get("name2")
	assert.Equal(t, "", res)

	res, err = txn5.Get("name3")
	assert.Equal(t, "here", res)
	res, err = txn3.Get("name3")
	assert.Equal(t, "", res)

	res, err = txn7.Get("name")
	assert.Equal(t, "v1", res)

	res, err = txn9.Get("name2")
	assert.Equal(t, "v5", res)

	res, err = txn11.Get("name")
	assert.Equal(t, "v3", res)

	res, err = txn13.Get("name2")
	assert.Equal(t, "v4", res)

	res, err = db.Get("name")
	assert.Equal(t, "v3", res)
	res, err = db.Get("name2")
	assert.Equal(t, "v4", res)
}

func TestCompactionKeepOnlyLatestVersionOfKeysIfNoTxnActive(t *testing.T) {
	defer dbDirCleanUp(t)
	db, err := NewDB(testDbConfig)
	assert.NoError(t, err)

	db.Put("name", "v1")
	db.Put("name", "v2")
	txn1, err := db.Begin()
	db.Put("name", "v3")
	db.Put("name2", "v4")
	db.Put("name2", "v5")
	txn2, err := db.Begin()
	db.Put("name2", "v6")

	db.FlushMemtable()
	db.ssTable.RunCompaction(math.MaxUint64)

	res, err := txn1.Get("name")
	assert.Equal(t, "", res)

	res, err = txn2.Get("name2")
	assert.Equal(t, "", res)

	res, err = db.Get("name")
	assert.Equal(t, "v3", res)
	res, err = db.Get("name2")
	assert.Equal(t, "v6", res)
}
