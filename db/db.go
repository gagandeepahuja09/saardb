package db

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/golang-db/memtable"
	sqlparser "github.com/golang-db/sql_parser"
	"github.com/golang-db/sstable"
	"github.com/golang-db/wal"
)

const (
	CatalogKey                               = "_calatog"
	SecondaryIndexesCatalogKeyTemplate       = "_secondary_indexes:%s"
	SchemaTemplate                           = "_schema:%s"
	IndexKeyTemplateTableNameIndexNamePrefix = "index:%s:%s"
	CmdPut                                   = "PUT"
)

type LocksAcquired struct {
	writerTxnId  uint64
	readerTxnIds []uint64
}
type transactionManager struct {
	nextTransactionId     uint64
	mu                    sync.Mutex
	keyVsLocksAcquiredMap map[string]*LocksAcquired
}

type DB struct {
	mu                   sync.RWMutex
	wal                  *wal.Wal
	memTable             *memtable.Memtable
	ssTable              *sstable.SsTable
	tableNameVsSchemaMap map[string]sqlparser.CreateTable
	transactionManager   transactionManager
}

type Config struct {
	SsTableConfig sstable.Config
	WalFilePath   string
}

func NewDB(config Config) (*DB, error) {
	db := DB{}
	wal, err := wal.NewWal(config.WalFilePath)
	if err != nil {
		return nil, err
	}
	db.wal = wal

	memTable, maxTxnId, err := db.buildMemtableFromWal()
	if err != nil {
		return nil, err
	}
	db.memTable = memTable
	db.ssTable, err = sstable.NewSsTable(config.SsTableConfig)
	if err != nil {
		return nil, err
	}

	db.tableNameVsSchemaMap, err = db.getTableNameVsSchemaMap()
	if err != nil {
		return nil, err
	}

	db.transactionManager = transactionManager{
		nextTransactionId:     maxTxnId + 1,
		mu:                    sync.Mutex{},
		keyVsLocksAcquiredMap: map[string]*LocksAcquired{},
	}

	return &db, err
}

func (db *DB) GetNextTransactionId() uint64 {
	return db.transactionManager.nextTransactionId
}

func (db *DB) getTableNameVsSchemaMap() (map[string]sqlparser.CreateTable, error) {
	tableNameVsSchemaMap := map[string]sqlparser.CreateTable{}
	tablesString, err := db.Get(CatalogKey)
	if err != nil {
		return nil, err
	}
	if tablesString == "" {
		return tableNameVsSchemaMap, nil
	}

	tableNames := strings.Split(tablesString, ",")
	for _, tableName := range tableNames {
		schemaStr, err := db.Get(fmt.Sprintf(SchemaTemplate, tableName))
		if err != nil {
			return nil, err
		}
		createTableInput, err := deserialiseCreateTableInput([]byte(schemaStr))
		if err != nil {
			return nil, err
		}
		createTableInput.TableName = tableName

		secondaryIndexesStr, err := db.Get(fmt.Sprintf(SecondaryIndexesCatalogKeyTemplate, tableName))
		if err != nil {
			return nil, err
		}
		secondaryIndexes, err := db.deserialiseSecondaryIndexCatalog(tableName, []byte(secondaryIndexesStr), createTableInput.ColumnDetails)
		if err != nil {
			return nil, err
		}
		createTableInput.SecondaryIndexes = secondaryIndexes

		tableNameVsSchemaMap[tableName] = *createTableInput
	}
	return tableNameVsSchemaMap, nil
}

func (db *DB) Close() {
	db.wal.Close()
	// todo: close all sstable files
}

func (db *DB) Get(key string) (value string, err error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	value, ok := db.memTable.Get(key)
	if !ok {
		value, err = db.ssTable.Get(key)
	}
	return value, err
}

func (db *DB) createSsTableAndClearWalAndMemTable() error {
	if err := db.flushMemtableToSsTable(); err != nil {
		return err
	}
	db.memTable.Clear()
	db.wal.Clear()
	return nil
}

func (db *DB) Put(key, value string) error {
	txn, err := db.Begin()
	if err != nil {
		return err
	}
	if err = txn.Put(key, value); err != nil {
		return err
	}
	return txn.Commit()
}

func (db *DB) flushMemtableToSsTable() error {
	// todo: when we start writing txnId to sstable, we also need to persist maxTxnId in manifest file.
	// and utilise that during application bootup to identify the maxTxnId.
	ssTableFile, err := db.ssTable.NewFile()
	if err != nil {
		return err
	}

	err = db.ssTable.Write(ssTableFile, db.memTable.Iterate)
	if db.ssTable.ShouldRunCompaction() {
		go db.ssTable.RunCompaction()
	}
	return err
}

func (db *DB) writeToWal(key, value string) error {
	buf := serialisePutCommand(key, value)
	return db.wal.WriteEntry(buf)
}

func appendLengthPrefixedString(buf []byte, value string) []byte {
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(value)))
	buf = append(buf, []byte(value)...)
	return buf
}

func readUint64(buf []byte, offset *int) (uint64, error) {
	if len(buf)-*offset < 8 {
		return 0, errors.New("malformed WAL command: missing uint64")
	}
	value := binary.BigEndian.Uint64(buf[*offset : *offset+8])
	*offset += 8
	return value, nil
}

func readUint32(buf []byte, offset *int) (uint32, error) {
	if len(buf)-*offset < 4 {
		return 0, errors.New("malformed WAL command: missing uint32")
	}
	value := binary.BigEndian.Uint32(buf[*offset : *offset+4])
	*offset += 4
	return value, nil
}

func readLengthPrefixedString(buf []byte, offset *int) (string, error) {
	valueLen, err := readUint32(buf, offset)
	if err != nil {
		return "", err
	}
	if valueLen > uint32(len(buf)-*offset) {
		return "", errors.New("malformed WAL command: string length exceeds payload")
	}
	value := string(buf[*offset : *offset+int(valueLen)])
	*offset += int(valueLen)
	return value, nil
}

func serialisePutCommand(key, value string) []byte {
	buf := []byte{}
	buf = appendLengthPrefixedString(buf, CmdPut)
	buf = appendLengthPrefixedString(buf, key)
	buf = appendLengthPrefixedString(buf, value)
	return buf
}

func deserialisePutCommand(buf []byte, offset *int) (key, value string, err error) {
	key, err = readLengthPrefixedString(buf, offset)
	if err != nil {
		return "", "", err
	}
	value, err = readLengthPrefixedString(buf, offset)
	if err != nil {
		return "", "", err
	}
	if *offset != len(buf) {
		return "", "", errors.New("malformed WAL command: unexpected trailing bytes")
	}
	return key, value, nil
}

func (db *DB) buildMemtableFromWal() (*memtable.Memtable, uint64, error) {
	memTable := memtable.NewMemtable()
	var maxTxnId uint64 = 0
	for {
		payload, err := db.wal.ReadEntry()
		if err == io.EOF {
			return &memTable, maxTxnId, nil
		}
		// for now, I will abort even in case of partial write
		// todo: in case of partial write we should just truncate that log.
		// we can also do that as part of listening to signal SIGTERM and SIGKILL?
		if err != nil {
			return nil, 0, err
		}

		offset := 0
		cmd, err := readLengthPrefixedString(payload, &offset)
		if err != nil {
			return nil, 0, err
		}
		switch cmd {
		case CmdTransaction:
			txnId, putCmds, err := deserialiseTransactionCommand(payload[offset:])
			if err != nil {
				return nil, 0, err
			}
			maxTxnId = max(maxTxnId, txnId)
			for _, cmd := range putCmds {
				memTable.Put(cmd.key, cmd.value, txnId)
			}
		default:
			return nil, 0, fmt.Errorf("unknown WAL command: %s", cmd)
		}
	}
}

func (db *DB) Begin() (*Transaction, error) {
	db.transactionManager.mu.Lock()
	defer db.transactionManager.mu.Unlock()

	txn := Transaction{
		id: db.transactionManager.nextTransactionId,
		db: db,
	}
	db.transactionManager.nextTransactionId++
	return &txn, nil
}

func (db *DB) InsertIntoTable(query string) error {
	parser := sqlparser.NewParser(query)
	input, err := parser.ParseInsertIntoTable()
	if err != nil {
		return err
	}
	return db.insertIntoTable(*input)
}

// key: table_name:primary_key_value
// value: [value1][size_of_value2][value2][value3]
// value1 and value2 are fixed sized datatype like int and bool while value2 is variable sized
// datatype like string.
// todo: value of primary_key is stored unnecessarily twice (both in key and value)
// todo: lexicographic ordering is currently as per string: 100 will come before 11. this won't
// work for SELECT range queries.
func (db *DB) serialiseInsertIntoTableInput(insertIntoTableInput sqlparser.InsertIntoTable) (
	key string, valueSchemaBuf []byte, err error) {
	tableName := insertIntoTableInput.TableName
	table := db.tableNameVsSchemaMap[tableName]
	primaryKeyValue := ""
	for i, columnValue := range insertIntoTableInput.ColumnValues {
		if i == table.PrimaryKeyColumnPosition {
			primaryKeyValue = columnValue
		}
		switch table.ColumnDetails[i].DataType {
		case sqlparser.Int:
			valueInt, err := strconv.Atoi(columnValue)
			if err != nil {
				return "", nil, err
			}
			valueSchemaBuf = binary.BigEndian.AppendUint32(valueSchemaBuf, uint32(valueInt))
		case sqlparser.String:
			valueSchemaBuf = binary.BigEndian.AppendUint32(valueSchemaBuf, uint32(len(columnValue)))
			valueSchemaBuf = append(valueSchemaBuf, []byte(columnValue)...)
		case sqlparser.Bool:
			// only 0, 1 supported and not true, false
			valueInt, err := strconv.Atoi(columnValue)
			if err != nil {
				return "", nil, err
			}
			if valueInt != 0 && valueInt != 1 {
				return "", nil, errors.New("only 0 and 1 values supported for BOOL data type")
			}
			valueSchemaBuf = append(valueSchemaBuf, uint8(valueInt))
		}
	}

	return fmt.Sprintf("%s:%s", tableName, primaryKeyValue), valueSchemaBuf, nil
}

func (db *DB) insertIntoTable(insertIntoTableInput sqlparser.InsertIntoTable) error {
	txn, err := db.Begin()
	if err != nil {
		return err
	}

	table := db.tableNameVsSchemaMap[insertIntoTableInput.TableName]
	if len(insertIntoTableInput.ColumnValues) != len(table.ColumnDetails) {
		return errors.New("INSERT INTO requires all columns to be present. ")
	}
	key, valueSchemaBuf, err := db.serialiseInsertIntoTableInput(insertIntoTableInput)
	if err != nil {
		return err
	}
	if err := txn.Put(key, string(valueSchemaBuf)); err != nil {
		txn.Rollback()
	}

	// todo: also test for the atomicity in the end-to-end test.
	err = db.updateSecondaryIndexes(insertIntoTableInput, txn)
	if err != nil {
		txn.Rollback()
	}

	txn.Commit()

	return nil
}

// generic function which can be used for both GET (pkColValue not available as found out after prefix)
// and PUT (pkColValue should always be present) operations
// Key structure `index:<table_name>:<index_name>:<column_value_1>:<column_value_2>:<pk_value_1>`
// pk_value_1 would be missing in prefix for GET
func getSecondaryIndexKeyOrPrefix(tableName, indexName string, columnValues []string, primaryKeyValue string) string {
	indexKey := fmt.Sprintf(IndexKeyTemplateTableNameIndexNamePrefix, tableName, indexName)
	indexColValues := ""
	for _, colValue := range columnValues {
		indexColValues += (":" + colValue)
	}
	indexKey += indexColValues
	if primaryKeyValue != "" {
		indexKey += (":" + primaryKeyValue)
	} else {
		indexKey += ":"
	}
	return indexKey
}

func (db *DB) getIndexAndPrimaryKeyColumnValuesInIndexSequence(indexColumnNames []string, insertIntoTableInput sqlparser.InsertIntoTable) ([]string, string, error) {
	table := db.tableNameVsSchemaMap[insertIntoTableInput.TableName]

	colValues := []string{}
	pkColValue := ""
	for _, indexColumnName := range indexColumnNames {
		for i, col := range table.ColumnDetails {
			if col.ColumnName == indexColumnName {
				colValues = append(colValues, insertIntoTableInput.ColumnValues[i])
			}
			if i == table.PrimaryKeyColumnPosition {
				pkColValue = insertIntoTableInput.ColumnValues[i]
			}
		}
	}
	if len(colValues) != len(indexColumnNames) {
		return nil, "", errors.New("all column values not found")
	}
	if pkColValue == "" {
		return nil, "", errors.New("primary key column value not found")
	}
	return colValues, pkColValue, nil
}

func (db *DB) updateSecondaryIndexes(insertIntoTableInput sqlparser.InsertIntoTable, txn *Transaction) error {
	table := db.tableNameVsSchemaMap[insertIntoTableInput.TableName]
	secondaryIndexes := table.SecondaryIndexes

	for _, secondaryIndex := range secondaryIndexes {
		colValues, pkColValue, err := db.getIndexAndPrimaryKeyColumnValuesInIndexSequence(secondaryIndex.Columns, insertIntoTableInput)
		if err != nil {
			return err
		}
		secondaryIndexKey := getSecondaryIndexKeyOrPrefix(insertIntoTableInput.TableName, secondaryIndex.IndexName, colValues, pkColValue)
		txn.Put(secondaryIndexKey, "")
	}
	return nil
}

func (db *DB) ShowTables() []string {
	tableNames := []string{}
	for _, table := range db.tableNameVsSchemaMap {
		tableNames = append(tableNames, table.TableName)
	}
	return tableNames
}

func (db *DB) ShowCreateTable(tableName string) (*sqlparser.CreateTable, error) {
	for _, table := range db.tableNameVsSchemaMap {
		if table.TableName == tableName {
			return &table, nil
		}
	}
	return nil, fmt.Errorf("table: '%s' not found", tableName)
}
