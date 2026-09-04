package main

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/golang-db/db"
	sqlparser "github.com/golang-db/sql_parser"
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

func buildTestDataForRepeatKeys(dbInstance *db.DB, innerLoopCount int) {
	for j := 0; j < 12; j++ {
		for i := 0; i < innerLoopCount; i++ {
			key := fmt.Sprintf("key_%d", i)
			value := fmt.Sprintf("value_%d", i+j)
			dbInstance.Put(key, value)
		}
	}
}

func buildTestDataForRepeatKeysInTable(db *db.DB, t *testing.T, loopCount int) {
	err := db.CreateTable("CREATE TABLE students (age INT, id STRING, isActive BOOL, PRIMARY KEY (id));")
	assert.NoError(t, err)

	ageValues := []int{10, 15, 20, 25}

	for j := 0; j < 4; j++ {
		for i := 0; i < loopCount; i++ {
			err = db.InsertIntoTable(fmt.Sprintf("INSERT INTO students VALUES (%d, id%d, 1)", ageValues[(i+j)%4], i))
			assert.NoError(t, err)
		}
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

func getExpectedIdsPerAge(loopCount int) map[int][]string {
	ageValues := []int{10, 15, 20, 25}

	// outer loop will run 4 times, the final value would be as per j = 3
	expectedIdsPerAge := map[int][]string{}
	for i := 0; i < loopCount; i++ {
		// outer loop will run 4 times, the final value would be as per j = 3
		age := ageValues[(i+3)%4]
		expectedIdsPerAge[age] = append(expectedIdsPerAge[age], fmt.Sprintf("id%d", i))
	}

	for age, expectedIds := range expectedIdsPerAge {
		sort.Strings(expectedIds)
		expectedIdsPerAge[age] = expectedIds
	}
	return expectedIdsPerAge
}

func assertAgeValuesFromDbSelect(t *testing.T, db *db.DB, expectedIdsPerAge map[int][]string) {
	ageValues := []int{10, 15, 20, 25}

	for _, age := range ageValues {
		res, err := db.SelectFromTable(sqlparser.SelectFromTable{
			TableName:       "students",
			ColumnsRequired: []string{"*"},
			QueryConditions: []sqlparser.QueryCondition{{
				ColumnName: "age",
				QueryType:  "=",
				Value:      fmt.Sprintf("%d", age),
			}},
		})
		assert.NoError(t, err)

		actualIds := []string{}
		for _, row := range res {
			actualIds = append(actualIds, row[1])
		}
		assert.Len(t, actualIds, len(expectedIdsPerAge[age]))
		sort.Strings(actualIds)
		assert.Equal(t, expectedIdsPerAge[age], actualIds)
	}
}

func TestSsTablePrefixScanPicksLatestTxnIdWithCompaction(t *testing.T) {
	defer dbDirCleanUp(t)

	db, err := db.NewDB(testDbConfig)
	buildTestDataForRepeatKeysInTable(db, t, 10)
	assert.NoError(t, err)

	expectedIdsPerAge := getExpectedIdsPerAge(10)
	assertAgeValuesFromDbSelect(t, db, expectedIdsPerAge)
}

func TestSsTablePrefixScanPicksLatestTxnIdWithoutCompaction(t *testing.T) {
	defer dbDirCleanUp(t)

	db, err := db.NewDB(testDbConfig)
	buildTestDataForRepeatKeysInTable(db, t, 10)
	assert.NoError(t, err)

	expectedIdsPerAge := getExpectedIdsPerAge(10)
	assertAgeValuesFromDbSelect(t, db, expectedIdsPerAge)
}

func TestSsTablePrefixScanPicksLatestTxnIdWithCompactionAndAfterApplicationRestart(t *testing.T) {
	defer dbDirCleanUp(t)

	dbForPut, err := db.NewDB(testDbConfig)
	buildTestDataForRepeatKeysInTable(dbForPut, t, 50)
	assert.NoError(t, err)

	expectedIdsPerAge := getExpectedIdsPerAge(50)

	dbAfterRestart, err := db.NewDB(testDbConfig)
	assertAgeValuesFromDbSelect(t, dbAfterRestart, expectedIdsPerAge)
}
