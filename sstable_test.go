package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-db/db"
	"github.com/golang-db/sstable"
	"github.com/stretchr/testify/assert"
)

var testDbConfig = db.Config{
	// todo: walConfig. will be better to have a single folder like: temp -> wal.log and sstable_datafiles
	// directory
	SsTableConfig: sstable.Config{
		DataFilesDirectory: "temp",
	},
}

func dbDirCleanUp(t *testing.T) {
	err := os.RemoveAll("temp")
	os.Remove("wal.log")
	assert.NoError(t, err)
}

func buildTestData(db *db.DB) {
	for i := 0; i < 300; i++ {
		key := fmt.Sprintf("key_%d", i)
		value := fmt.Sprintf("value_%d", i)
		db.Put(key, value)
	}

	time.Sleep(100 * time.Millisecond)
	for i := 300; i <= 377; i++ {
		key := fmt.Sprintf("key_%d", i)
		value := fmt.Sprintf("value_%d", i)
		db.Put(key, value)
	}
}

func assertValuesForTestData(t *testing.T, db *db.DB) {
	for i := 250; i <= 377; i++ {
		value, err := db.Get(fmt.Sprintf("key_%d", i))
		assert.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("value_%d", i), value)
	}

	for i := 600; i <= 625; i++ {
		value, _ := db.Get(fmt.Sprintf("key_%d", i))
		assert.Equal(t, "", value)
	}
}

// we take large enough keys so that the flow can be tested for flushing memtable to sstable
func TestGetAndPutInBulk(t *testing.T) {
	defer dbDirCleanUp(t)

	db, err := db.NewDB(testDbConfig)
	assert.NoError(t, err)
	buildTestData(db)

	// let the old unrequired files which should now be compacted to a single file get deleted
	time.Sleep(4 * time.Second)

	value, err := db.Get("key_101")
	assert.NoError(t, err)
	assert.Equal(t, "value_101", value)

	value, err = db.Get("key_1010")
	assert.Equal(t, "", value)

	value, err = db.Get("key_10100")
	assert.Equal(t, "", value)

	value, err = db.Get("GET")
	assert.Equal(t, "", value)

	assertValuesForTestData(t, db)
}

func buildTestDataForRepeatKeys(dbInstance *db.DB, innerLoopCount int) {
	for j := 0; j < 12; j++ {
		for i := 0; i < innerLoopCount; i++ {
			key := fmt.Sprintf("key_%d", i)
			value := fmt.Sprintf("value_%d", i+j)
			dbInstance.Put(key, value)
		}
	}
}

// write the same set of keys with multiple versions (txnId)
func TestSsTableGetPicksLatestTxnIdWithCompaction(t *testing.T) {
	defer dbDirCleanUp(t)

	db, err := db.NewDB(testDbConfig)
	buildTestDataForRepeatKeys(db, 50)
	assert.NoError(t, err)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key_%d", i)
		val, err := db.Get(key)
		expectedValue := fmt.Sprintf("value_%d", i+11)
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, val)
	}
}

func TestSsTableGetPicksLatestTxnIdWithoutCompaction(t *testing.T) {
	defer dbDirCleanUp(t)

	db, err := db.NewDB(testDbConfig)
	buildTestDataForRepeatKeys(db, 15)
	assert.NoError(t, err)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key_%d", i)
		val, err := db.Get(key)
		assert.NoError(t, err)
		expectedValue := fmt.Sprintf("value_%d", i+11)
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, val)
	}
}
