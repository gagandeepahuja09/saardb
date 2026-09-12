package db

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golang-db/sstable"
	"github.com/stretchr/testify/assert"
)

func newDBForTest() (*DB, func(), error) {
	dbInstance, err := NewDB(Config{
		SsTableConfig: sstable.Config{
			DataFilesDirectory: "temp",
		},
		WalFilePath: "temp_wal.log",
	})
	cleanupFunc := func() {
		defer os.RemoveAll("temp")
		defer os.Remove("temp_wal.log")
	}
	return dbInstance, cleanupFunc, err
}

// same transaction get -> put -> get should read from buffered writes
func TestSameTransactionPutAndGet(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_101"
	expectedValue := "value_101"

	val, err := txn.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, "", val)

	err = txn.Put(testKey, expectedValue)
	assert.Nil(t, err)

	val, err = txn.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedValue, val)
}

// t1 acquires read lock first. t1 upgrades to write lock. t2 will still able to acquire read lock.
func TestDifferentTransactionPutAndGetWithPutAcquiringLock(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_101"
	expectedValue := "value_101"

	_, err = txn.Get(testKey)
	assert.Nil(t, err)

	err = txn.Put(testKey, expectedValue)
	assert.Nil(t, err)

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	_, err = txn2.Get(testKey)
	assert.NoError(t, err)
}

// t1 acquires read lock first, t2 will be able to acquire write lock
func TestDifferentTransactionPutAndGetWithGetAcquiringLock(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_101"

	_, err = txn.Get(testKey)
	assert.Nil(t, err)

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	err = txn2.Put(testKey, "value")
	assert.NoError(t, err)
}

// multiple transactions should be able to acquire read lock for a key at the same time
func TestMultipleOpenTransactionsGetSameKey(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	testKey := "key_101"
	expectedValue := "value_101"
	err = dbInstance.Put(testKey, expectedValue)
	assert.Nil(t, err)

	for i := 0; i < 10; i++ {
		txn, err := dbInstance.Begin()
		assert.Nil(t, err)

		val, err := txn.Get(testKey)
		assert.Nil(t, err)
		assert.Equal(t, expectedValue, val)
	}
}

// if t1 and t2 are calling put on different keys, it should not lead to any conflict
func TestDifferentOpenTransactionPutWithDifferentKeys(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_101"
	expectedValue := "value_101"

	err = txn.Put(testKey, expectedValue)
	assert.Nil(t, err)

	val, err := txn.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedValue, val)

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	err = txn2.Put("key_102", expectedValue)
	assert.Nil(t, err)
}

// if t1 and t2 are calling put on the same key, t2 (which tried acquiring later)
// should get an error
func TestDifferentOpenTransactionPutWithSameKey(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_101"
	expectedValue := "value_101"

	err = txn.Put(testKey, expectedValue)
	assert.Nil(t, err)

	val, err := txn.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedValue, val)

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	err = txn2.Put(testKey, expectedValue)
	assert.Equal(t, "cannot acquire write lock as write lock acquired by transaction '2'", err.Error())
}

// t1 acquires write lock, t2 will be able to acquire read lock and reads old value
// t2 rollsback, t2 will be able to read and reads an old value
func TestRollbackReleasesLockAndCleansUpBufferedWrite(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	testKey := "key_105"
	expectedValue := "value_105"

	err = txn.Put(testKey, expectedValue)
	assert.Nil(t, err)

	val, err := txn.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedValue, val)

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	val, err = txn2.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, "", val)

	txn.Rollback()

	val, err = txn2.Get(testKey)
	assert.Nil(t, err)
	assert.Equal(t, "", val)
}

// t1 acquires write lock and does multiple write operations
// t2 will be able to read values of all of the keys and gets empty result
// t1 commits
// t3 starts in a new db instance requiring to build memtable from wal during init
// reads from t3 return all updated values as per updates by t1
func TestCommitReleasesLockAndPersistsAllWrites(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.Nil(t, err)

	txn, err := dbInstance.Begin()
	assert.Nil(t, err)

	for i := 200; i <= 210; i++ {
		err = txn.Put(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
		assert.Nil(t, err)
	}

	txn2, err := dbInstance.Begin()
	assert.Nil(t, err)

	for i := 200; i <= 210; i++ {
		val, err := txn2.Get(fmt.Sprintf("key_%d", i))
		assert.Nil(t, err)
		assert.Equal(t, "", val)
	}

	txn.Commit()

	// create a new db instance after commit. this will also test the deserialisation logic during
	// application init (buildMemtableFromWal) specifically for deserialiseTransactionCommand.
	dbInstance2, _, err := newDBForTest()
	txn3, err := dbInstance2.Begin()
	assert.Nil(t, err)

	for i := 200; i <= 210; i++ {
		val, err := txn3.Get(fmt.Sprintf("key_%d", i))
		assert.Nil(t, err)
		assert.Equal(t, fmt.Sprintf("value_%d", i), val)
	}
}

// t1 to t11: 11 transactions parallely try to acquire write lock, only 1 should succeed.
// when all of them try to read, all should succeed
// after commit, also assert the value with db.Get.
func TestPutAndGetRaceConditionOnlyOneShouldAcquireWriteLock(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	assert.Nil(t, err)
	defer cleanupFunc()

	txns := []*Transaction{}

	for i := 0; i <= 10; i++ {
		txn, err := dbInstance.Begin()
		assert.Nil(t, err)
		txns = append(txns, txn)
	}

	commonKey := "key_282"

	var wg sync.WaitGroup
	wg.Add(11)

	var putErrCount atomic.Int32
	var getErrCount atomic.Int32

	expectedValue := ""
	putSucceededTxn := 0

	for i := 0; i <= 10; i++ {
		go func() {
			putErr := txns[i].Put(commonKey, fmt.Sprintf("value_%d", i))
			if putErr != nil {
				hasExpectedErrorPrefix := strings.HasPrefix(putErr.Error(), "cannot acquire write lock as write lock acquired by transaction")
				assert.True(t, hasExpectedErrorPrefix)
				putErrCount.Add(1)
			} else {
				putSucceededTxn = i
			}

			val, getErr := txns[i].Get(commonKey)
			if getErr != nil {
				hasExpectedErrorPrefix := strings.HasPrefix(getErr.Error(), "cannot acquire read lock as write lock acquired by transaction")
				assert.True(t, hasExpectedErrorPrefix)
				getErrCount.Add(1)
			} else {
				if i == putSucceededTxn {
					expectedValue = val
					assert.Equal(t, fmt.Sprintf("value_%d", i), val)
				} else {
					assert.Equal(t, "", val)
				}
			}
			txns[i].Commit()
			wg.Done()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(10), putErrCount.Load())
	assert.Equal(t, int32(0), getErrCount.Load())

	val, err := dbInstance.Get(commonKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedValue, val)
}
