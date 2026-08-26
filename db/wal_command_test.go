package db

import (
	"path/filepath"
	"testing"

	"github.com/golang-db/sstable"
	"github.com/stretchr/testify/assert"
)

func newDBForWalCommandTest(t *testing.T) (*DB, Config) {
	t.Helper()

	dir := t.TempDir()
	config := Config{
		SsTableConfig: sstable.Config{
			DataFilesDirectory: filepath.Join(dir, "sstable"),
		},
		WalFilePath: filepath.Join(dir, "wal.log"),
	}

	dbInstance, err := NewDB(config)
	assert.NoError(t, err)
	return dbInstance, config
}

func closeDBOnce(dbInstance *DB) func() {
	closed := false
	return func() {
		if closed {
			return
		}
		dbInstance.Close()
		closed = true
	}
}

func TestSerialisePutCommandRoundTrip(t *testing.T) {
	payload := serialisePutCommand("key with spaces", "value with spaces\nand newline")

	offset := 0
	cmd, err := readLengthPrefixedString(payload, &offset)
	assert.NoError(t, err)
	assert.Equal(t, CmdPut, cmd)

	key, value, err := deserialisePutCommand(payload, &offset)
	assert.NoError(t, err)
	assert.Equal(t, "key with spaces", key)
	assert.Equal(t, "value with spaces\nand newline", value)
}

func TestReadLengthPrefixedStringMalformedPayload(t *testing.T) {
	testCases := []struct {
		name        string
		buf         []byte
		expectedErr string
	}{
		{
			name:        "missing length",
			buf:         []byte{0, 0, 0},
			expectedErr: "malformed WAL command: missing uint32",
		},
		{
			name:        "string length exceeds payload",
			buf:         []byte{0, 0, 0, 5, 'a'},
			expectedErr: "malformed WAL command: string length exceeds payload",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			offset := 0
			_, err := readLengthPrefixedString(tt.buf, &offset)
			assert.EqualError(t, err, tt.expectedErr)
		})
	}
}

func TestDeserialisePutCommandRejectsTrailingBytes(t *testing.T) {
	// A valid PUT command should consume the full WAL payload. The extra byte
	// simulates malformed data that the parser must not silently ignore.
	payload := append(serialisePutCommand("key", "value"), 'x')

	offset := 0
	cmd, err := readLengthPrefixedString(payload, &offset)
	assert.NoError(t, err)
	assert.Equal(t, CmdPut, cmd)

	_, _, err = deserialisePutCommand(payload, &offset)
	assert.EqualError(t, err, "malformed WAL command: unexpected trailing bytes")
}

func TestSerialiseTransactionCommitPayloadRoundTrip(t *testing.T) {
	payload := serialiseTransactionCommitPayload(map[string]string{
		"txn key with spaces": "txn value with spaces",
		"txn key newline":     "txn value\nwith newline",
	}, 4)

	offset := 0
	cmd, err := readLengthPrefixedString(payload, &offset)
	assert.NoError(t, err)
	assert.Equal(t, CmdTransaction, cmd)

	txnId, putCmds, err := deserialiseTransactionCommand(payload[offset:])
	assert.Equal(t, uint64(4), txnId)
	assert.NoError(t, err)

	actual := map[string]string{}
	for _, putCmd := range putCmds {
		actual[putCmd.key] = putCmd.value
	}

	assert.Equal(t, map[string]string{
		"txn key with spaces": "txn value with spaces",
		"txn key newline":     "txn value\nwith newline",
	}, actual)
}

func TestDeserialiseTransactionCommandRejectsMalformedPayloadMissingTxnId(t *testing.T) {
	_, _, err := deserialiseTransactionCommand([]byte{0, 0, 0, 0, 0, 0, 0})
	assert.EqualError(t, err, "malformed WAL command: missing uint64")
}

func TestDeserialiseTransactionCommandRejectsMalformedPayloadMissingNumWrites(t *testing.T) {
	_, _, err := deserialiseTransactionCommand([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	assert.EqualError(t, err, "malformed WAL command: missing uint32")
}

func TestDBRecoversPutValuesWithSpacesAndNewlines(t *testing.T) {
	dbInstance, config := newDBForWalCommandTest(t)
	closeDB := closeDBOnce(dbInstance)
	defer closeDB()

	expected := map[string]string{
		"simple":          "value with spaces",
		"key with spaces": "value with\nnewline",
	}
	for key, value := range expected {
		assert.NoError(t, dbInstance.Put(key, value))
	}
	closeDB()

	dbAfterRestart, err := NewDB(config)
	assert.NoError(t, err)
	defer dbAfterRestart.Close()

	for key, expectedValue := range expected {
		value, err := dbAfterRestart.Get(key)
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, value)
	}
}

func TestDBRecoversLatestValueAfterOverwrite(t *testing.T) {
	dbInstance, config := newDBForWalCommandTest(t)
	closeDB := closeDBOnce(dbInstance)
	defer closeDB()

	assert.NoError(t, dbInstance.Put("same key", "old value"))
	assert.NoError(t, dbInstance.Put("same key", "new value\nwith newline"))
	closeDB()

	dbAfterRestart, err := NewDB(config)
	assert.NoError(t, err)
	defer dbAfterRestart.Close()

	value, err := dbAfterRestart.Get("same key")
	assert.NoError(t, err)
	assert.Equal(t, "new value\nwith newline", value)
}

func TestDBRecoversTransactionValuesWithSpacesAndNewlines(t *testing.T) {
	dbInstance, config := newDBForWalCommandTest(t)
	closeDB := closeDBOnce(dbInstance)
	defer closeDB()

	txn, err := dbInstance.Begin()
	assert.NoError(t, err)
	assert.NoError(t, txn.Put("txn key with spaces", "txn value with spaces"))
	assert.NoError(t, txn.Put("txn key newline", "txn value\nwith newline"))
	assert.NoError(t, txn.Commit())
	closeDB()

	dbAfterRestart, err := NewDB(config)
	assert.NoError(t, err)
	assert.Equal(t, uint64(2), dbAfterRestart.GetNextTransactionId())
	defer dbAfterRestart.Close()

	value, err := dbAfterRestart.Get("txn key with spaces")
	assert.NoError(t, err)
	assert.Equal(t, "txn value with spaces", value)

	value, err = dbAfterRestart.Get("txn key newline")
	assert.NoError(t, err)
	assert.Equal(t, "txn value\nwith newline", value)
}
