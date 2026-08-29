package db

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	sqlparser "github.com/golang-db/sql_parser"
)

func isPointedPrimaryKeyQuery(selectFromTableInput sqlparser.SelectFromTable, pkColumnName string) bool {
	return len(selectFromTableInput.QueryConditions) == 1 && selectFromTableInput.QueryConditions[0].ColumnName == pkColumnName &&
		selectFromTableInput.QueryConditions[0].QueryType == sqlparser.Equals
}

func isFullTableScanQuery(selectFromTableInput sqlparser.SelectFromTable) bool {
	return len(selectFromTableInput.QueryConditions) == 0
}

func (txn *Transaction) getRowForPrimaryKey(tableName, primaryKeyId string) ([]string, error) {
	key := fmt.Sprintf("%s:%s", tableName, primaryKeyId)
	value, err := txn.Get(key)
	if err != nil {
		return nil, err
	}
	rowValues, err := txn.db.deserializeRowValues(tableName, value)
	if err != nil {
		return nil, err
	}
	return rowValues, nil
}

// go through each secondary index. check if any of the secondary index can independently cover all WHERE conditions
// if not ALL conditions, check if some of the conditions from the prefix of the WHERE condition can be covered.
// returns nil if no secondary index is applicable
func getSecondaryIndexForQueryIfApplicable(selectFromTableInput sqlparser.SelectFromTable, secondaryIndexes []sqlparser.SecondaryIndex) (*sqlparser.SecondaryIndex, []string) {
	var candidateSecondaryIndex *sqlparser.SecondaryIndex
	colsCoveredInCandidateSecondaryIndex := []string{}
	for _, secondaryIndex := range secondaryIndexes {
		secIdxColsCoveredFromInputQuery := []string{}
		inputQueryColumn := map[string]bool{}
		for _, qc := range selectFromTableInput.QueryConditions {
			inputQueryColumn[qc.ColumnName] = true
		}
		for _, secIdxCol := range secondaryIndex.Columns {
			if inputQueryColumn[secIdxCol] {
				secIdxColsCoveredFromInputQuery = append(secIdxColsCoveredFromInputQuery, secIdxCol)
			} else {
				// we are going through each column in the secondary index sequentially
				// and as soon as we find a secondary index column which is not
				// part of the input SELECT query conditions, we break.
				// this is crucial because composite index requires prefix match and even some of the
				// prefix getting covered is good for choosing an index.
				break
			}
			if len(secIdxColsCoveredFromInputQuery) == len(secondaryIndex.Columns) {
				return &secondaryIndex, secIdxColsCoveredFromInputQuery
			}
			if len(secIdxColsCoveredFromInputQuery) > 0 {
				candidateSecondaryIndex = &secondaryIndex
				colsCoveredInCandidateSecondaryIndex = secIdxColsCoveredFromInputQuery
			}
		}
	}
	return candidateSecondaryIndex, colsCoveredInCandidateSecondaryIndex
}

func allColumnsCoveredBySecondaryIndex(secondaryIndex *sqlparser.SecondaryIndex, colsCoveredInSecIndex []string) bool {
	return len(secondaryIndex.Columns) == len(colsCoveredInSecIndex)
}

func isQueryConditionApplicable(row []string, colPos int, qc sqlparser.QueryCondition) (bool, error) {
	switch qc.QueryType {
	case sqlparser.Equals:
		return row[colPos] == qc.Value, nil
	case sqlparser.Lt:
		return row[colPos] < qc.Value, nil
	case sqlparser.Lte:
		return row[colPos] <= qc.Value, nil
	case sqlparser.Gt:
		return row[colPos] > qc.Value, nil
	case sqlparser.Gte:
		return row[colPos] >= qc.Value, nil
	}
	return false, errors.New("query type not supported")
}

// filters on the basis of all applicable conditions in []sqlparser.QueryCondition.
// if query result is already covered by secondary index, that filter is not applied.
func (db *DB) filterQueryConditions(tableName string, queryConditions []sqlparser.QueryCondition,
	colsCoveredInSecIndex []string, queryResult [][]string) ([][]string, error) {
	for _, qc := range queryConditions {
		colName := qc.ColumnName
		if slices.Contains(colsCoveredInSecIndex, colName) {
			continue
		}
		colPos := db.getColPositionFromColName(tableName, colName)
		if colPos == -1 {
			return nil, errors.New("unable to apply all query conditions")
		}
		filteredQueryResult := [][]string{}
		for _, row := range queryResult {
			applicable, err := isQueryConditionApplicable(row, colPos, qc)
			if err != nil {
				return nil, err
			}
			if applicable {
				filteredQueryResult = append(filteredQueryResult, row)
			}
		}
		queryResult = filteredQueryResult
	}
	return queryResult, nil
}

func (db *DB) runFullTableScanAndFilterConditions(tableName string, selectFromTableInput sqlparser.SelectFromTable) ([][]string, error) {
	queryResult, err := db.fullTableScan(tableName)
	if err != nil {
		return nil, err
	}
	return db.filterQueryConditions(tableName, selectFromTableInput.QueryConditions,
		[]string{}, queryResult)
}

// todo: not solving for RANGE queries within secondary index or primary key right now.
func (txn *Transaction) getQueryResultFromSecondaryIndexIfApplicable(tableName string, selectFromTableInput sqlparser.SelectFromTable, schema sqlparser.CreateTable) ([][]string, error) {
	secondaryIndex, colsCoveredInSecIndex := getSecondaryIndexForQueryIfApplicable(selectFromTableInput, schema.SecondaryIndexes)
	if secondaryIndex == nil {
		return txn.db.runFullTableScanAndFilterConditions(tableName, selectFromTableInput)
	}
	indexCoveredColValues := []string{}

	for _, colName := range colsCoveredInSecIndex {
		for _, condition := range selectFromTableInput.QueryConditions {
			if condition.ColumnName == colName {
				indexCoveredColValues = append(indexCoveredColValues, condition.Value)
			}
		}
	}
	prefixKey := getSecondaryIndexKeyOrPrefix(tableName, secondaryIndex.IndexName, indexCoveredColValues, "")
	primaryKeyIds, err := txn.db.secondaryIndexPrefixScan(prefixKey)
	if err != nil {
		return nil, err
	}
	// Run GET query for each primary key id separately and combine the result of each.
	queryResult := [][]string{}
	for _, pkId := range primaryKeyIds {
		rowValues, err := txn.getRowForPrimaryKey(tableName, pkId)
		if err != nil {
			return nil, err
		}
		queryResult = append(queryResult, rowValues)
	}
	if allColumnsCoveredBySecondaryIndex(secondaryIndex, colsCoveredInSecIndex) {
		return queryResult, nil
	}
	return txn.db.filterQueryConditions(tableName, selectFromTableInput.QueryConditions,
		colsCoveredInSecIndex, queryResult)
}

func (db *DB) selectFromTable(selectFromTableInput sqlparser.SelectFromTable) ([][]string, error) {
	txn, err := db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := txn.selectFromTable(selectFromTableInput)
	if err != nil {
		txn.Rollback()
		return nil, err
	}
	err = txn.Commit()
	return res, err
}

// todo: without index scan, AND queries support to be added.
func (txn *Transaction) selectFromTable(selectFromTableInput sqlparser.SelectFromTable) ([][]string, error) {
	db := txn.db
	tableName := selectFromTableInput.TableName
	schema, ok := db.tableNameVsSchemaMap[selectFromTableInput.TableName]
	pkPos := schema.PrimaryKeyColumnPosition
	pkColumnName := ""
	if !ok {
		return nil, fmt.Errorf("table with name %q not found", tableName)
	}
	// improve performance by storing primary key column name also apart from primary key column position?
	for i, col := range schema.ColumnDetails {
		if i == pkPos {
			pkColumnName = col.ColumnName
		}
	}
	if pkColumnName == "" {
		return nil, errors.New("primary key column position is incorrect")
	}
	// todo: not solving for returning limited columns right now
	if isPointedPrimaryKeyQuery(selectFromTableInput, pkColumnName) {
		rowValues, err := txn.getRowForPrimaryKey(tableName, selectFromTableInput.QueryConditions[0].Value)
		if err != nil {
			return nil, err
		}
		return [][]string{rowValues}, nil
	}
	if isFullTableScanQuery(selectFromTableInput) {
		return db.fullTableScan(tableName)
	}

	return txn.getQueryResultFromSecondaryIndexIfApplicable(tableName, selectFromTableInput, schema)
}

// value: [value1][size_of_value2][value2][value3]
func (db *DB) deserializeRowValues(tableName, value string) ([]string, error) {
	// read byte inputs
	schema := db.tableNameVsSchemaMap[tableName]
	valueBuf := []byte(value)
	i := 0
	rowValues := []string{}
	for _, col := range schema.ColumnDetails {
		switch col.DataType {
		case sqlparser.Int:
			val := strconv.FormatUint(uint64(binary.BigEndian.Uint32(valueBuf[i:i+4])), 10)
			rowValues = append(rowValues, val)
			i += 4
		case sqlparser.String:
			len := int(binary.BigEndian.Uint32(valueBuf[i : i+4]))
			i += 4
			val := string(valueBuf[i : i+len])
			rowValues = append(rowValues, val)
			i += len

		case sqlparser.Bool:
			val := strconv.FormatUint(uint64(valueBuf[i]), 2)
			rowValues = append(rowValues, val)
			i++
		}
	}
	return rowValues, nil
}

func (db *DB) fullTableScan(tableName string) ([][]string, error) {
	key := fmt.Sprintf("%s:", tableName)
	memTableMap := db.memTable.PrefixScan(key)
	ssTableMap, err := db.ssTable.PrefixScan(key)
	if err != nil {
		return nil, err
	}

	scanOutput := [][]string{}
	for _, value := range memTableMap {
		values, err := db.deserializeRowValues(tableName, value)
		if err != nil {
			return nil, err
		}
		scanOutput = append(scanOutput, values)
	}

	for key, currValueTxnId := range ssTableMap {
		if _, ok := memTableMap[key]; ok {
			// memtable would have the most up-to date value. no need to rely on sstable value when key is found in memtable
			continue
		}
		values, err := db.deserializeRowValues(tableName, currValueTxnId.GetValue())
		if err != nil {
			return nil, err
		}
		scanOutput = append(scanOutput, values)
	}
	return scanOutput, nil
}

// returns an array of primary key IDs which satisfy the index.
func (db *DB) secondaryIndexPrefixScan(prefixKey string) ([]string, error) {
	memTableMap := db.memTable.PrefixScan(prefixKey)
	ssTableMap, err := db.ssTable.PrefixScan(prefixKey)
	if err != nil {
		return nil, err
	}

	pkSet := make(map[string]bool)

	primaryKeyIds := []string{}
	for key, _ := range ssTableMap {
		keyElements := strings.Split(key, ":")
		pk := keyElements[len(keyElements)-1]
		pkSet[pk] = true
	}
	for key, _ := range memTableMap {
		keyElements := strings.Split(key, ":")
		pk := keyElements[len(keyElements)-1]
		pkSet[pk] = true
	}
	for pk := range pkSet {
		primaryKeyIds = append(primaryKeyIds, pk)
	}
	return primaryKeyIds, nil
}
