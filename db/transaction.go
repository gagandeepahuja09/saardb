package db

import (
	"encoding/binary"
	"fmt"

	"errors"
)

const (
	WriteLockNotAcquiredDueToReadLocksError = "cannot acquire write lock as read lock acquired by one or more transactions"
	CmdTransaction                          = "TRANSACTION"
)

type Transaction struct {
	id               uint64
	db               *DB
	bufferedWriteMap map[string]string
	lockAcquiredKeys []string
}

type walPutCommand struct {
	key   string
	value string
}

func (txn *Transaction) tryAcquireWriteLock(key string) error {
	locksAcquired, ok := txn.db.transactionManager.keyVsLocksAcquiredMap[key]
	readLockAlreadyAcquired := false
	if !ok {
		locksAcquired = &LocksAcquired{}
	} else {
		writerTxnId := locksAcquired.writerTxnId
		if writerTxnId != 0 && writerTxnId != txn.id {
			// todo: can we have a wait feature where we wait for lock to be released instead of error?
			return fmt.Errorf("cannot acquire write lock as write lock acquired by transaction '%d'", writerTxnId)
		}
		if writerTxnId == txn.id {
			return nil
		}

		readerTxnIds := locksAcquired.readerTxnIds
		if len(readerTxnIds) > 1 {
			return errors.New(WriteLockNotAcquiredDueToReadLocksError)
		}
		if len(readerTxnIds) == 1 {
			if readerTxnIds[0] == txn.id {
				readLockAlreadyAcquired = true
				locksAcquired.readerTxnIds = []uint64{}
			} else {
				return errors.New(WriteLockNotAcquiredDueToReadLocksError)
			}
		}
	}
	locksAcquired.writerTxnId = txn.id
	if txn.lockAcquiredKeys == nil {
		txn.lockAcquiredKeys = []string{}
	}
	if !readLockAlreadyAcquired {
		txn.lockAcquiredKeys = append(txn.lockAcquiredKeys, key)
	}
	txn.db.transactionManager.keyVsLocksAcquiredMap[key] = locksAcquired
	return nil
}

func (txn *Transaction) tryAcquireReadLock(key string) error {
	locksAcquired, ok := txn.db.transactionManager.keyVsLocksAcquiredMap[key]
	writeLockAlreadyAcquired := false
	if !ok {
		locksAcquired = &LocksAcquired{}
	} else {
		writerTxnId := locksAcquired.writerTxnId
		if writerTxnId != 0 {
			if writerTxnId != txn.id {
				return fmt.Errorf("cannot acquire read lock as write lock acquired by transaction '%d'", writerTxnId)
			} else {
				writeLockAlreadyAcquired = true
			}
		}
	}
	for _, txnId := range locksAcquired.readerTxnIds {
		if txnId == txn.id {
			return nil
		}
	}
	locksAcquired.readerTxnIds = append(locksAcquired.readerTxnIds, txn.id)
	if txn.lockAcquiredKeys == nil {
		txn.lockAcquiredKeys = []string{}
	}
	if !writeLockAlreadyAcquired {
		txn.lockAcquiredKeys = append(txn.lockAcquiredKeys, key)
	}
	txn.db.transactionManager.keyVsLocksAcquiredMap[key] = locksAcquired
	return nil
}

// todo: optimisation for later: sharded locks
func (txn *Transaction) Put(key, value string) error {
	txn.db.transactionManager.mu.Lock()
	err := txn.tryAcquireWriteLock(key)
	txn.db.transactionManager.mu.Unlock()
	if err != nil {
		return err
	}

	if txn.bufferedWriteMap == nil {
		txn.bufferedWriteMap = map[string]string{}
	}
	txn.bufferedWriteMap[key] = value
	return nil
}

func (txn *Transaction) Get(key string) (string, error) {
	txn.db.transactionManager.mu.Lock()
	err := txn.tryAcquireReadLock(key)
	txn.db.transactionManager.mu.Unlock()
	if err != nil {
		return "", err
	}
	if value, ok := txn.bufferedWriteMap[key]; ok {
		return value, nil
	}
	return txn.db.Get(key)
}

func (txn *Transaction) releaseAllLocks() {
	for _, key := range txn.lockAcquiredKeys {
		locksAcquired := txn.db.transactionManager.keyVsLocksAcquiredMap[key]
		if locksAcquired.writerTxnId == txn.id {
			locksAcquired.writerTxnId = 0
		} else {
			currentTxnIdFound := false
			updatedReaderTxnIds := []uint64{}
			for _, readerTxnId := range locksAcquired.readerTxnIds {
				if readerTxnId == txn.id {
					currentTxnIdFound = true
					continue
				}
				updatedReaderTxnIds = append(updatedReaderTxnIds, readerTxnId)
			}
			if !currentTxnIdFound {
				fmt.Println("INCONSISTENCY_OBSERVED_lockAcquiredKeys_AND_keyVsLocksAcquiredMap", map[string]interface{}{
					"lockAcquiredKeys":      txn.lockAcquiredKeys,
					"keyVsLocksAcquiredMap": txn.db.transactionManager.keyVsLocksAcquiredMap[key],
					"key":                   key,
				})
			}
			locksAcquired.readerTxnIds = updatedReaderTxnIds
		}
	}
	txn.lockAcquiredKeys = []string{}
}

func (txn *Transaction) cleanupBufferedWriteMap() {
	txn.bufferedWriteMap = map[string]string{}
}

func (txn *Transaction) Rollback() {
	txn.releaseAllLocks()
	txn.cleanupBufferedWriteMap()
}

// payload structure:
// [length_of_command][command="TRANSACTION"][64_bit_transaction_id][number_of_writes]
// [key_length_for_1st][key_for_1st][value_length_for_1st][value_for_1st]...
func serialiseTransactionCommitPayload(writeMap map[string]string, txnId uint64) []byte {
	buf := []byte{}
	buf = appendLengthPrefixedString(buf, CmdTransaction)
	buf = binary.BigEndian.AppendUint64(buf, txnId)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(writeMap)))

	for key, value := range writeMap {
		buf = appendLengthPrefixedString(buf, key)
		buf = appendLengthPrefixedString(buf, value)
	}
	return buf
}

// [number_of_writes][key_length_for_1st][key_for_1st][value_length_for_1st][value_for_1st]...
func deserialiseTransactionCommand(buf []byte) (uint64, []walPutCommand, error) {
	offset := 0
	txnId, err := readUint64(buf, &offset)
	if err != nil {
		return 0, nil, err
	}
	numWrites, err := readUint32(buf, &offset)
	if err != nil {
		return 0, nil, err
	}
	putCmds := []walPutCommand{}
	for j := 0; j < int(numWrites); j++ {
		key, err := readLengthPrefixedString(buf, &offset)
		if err != nil {
			return 0, nil, err
		}
		value, err := readLengthPrefixedString(buf, &offset)
		if err != nil {
			return 0, nil, err
		}
		putCmds = append(putCmds, walPutCommand{
			key:   key,
			value: value,
		})
	}
	if offset != len(buf) {
		return 0, nil, errors.New("malformed WAL command: unexpected trailing bytes")
	}
	return txnId, putCmds, nil
}

// necessary to do in a single WAL write for atomicity
func (txn *Transaction) writeSingleWalEntryForCommit() error {
	buf := serialiseTransactionCommitPayload(txn.bufferedWriteMap, txn.id)
	return txn.db.wal.WriteEntry(buf)
}

func (txn *Transaction) Commit() error {
	if err := txn.writeSingleWalEntryForCommit(); err != nil {
		return err
	}

	// put in memtable done separately instead of db.Put as that would lead to separate writes in WAL
	for key, value := range txn.bufferedWriteMap {
		txn.db.memTable.Put(key, value, txn.id)
	}

	if txn.db.memTable.ShouldFlush() {
		if err := txn.db.createSsTableAndClearWalAndMemTable(); err != nil {
			return err
		}
	}

	txn.releaseAllLocks()
	txn.cleanupBufferedWriteMap()

	return nil
}
