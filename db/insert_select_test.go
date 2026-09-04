package db

import (
	"fmt"
	"strconv"
	"testing"

	sqlparser "github.com/golang-db/sql_parser"
	"github.com/stretchr/testify/assert"
)

func TestInsertSelectTestWithPrimaryKey(t *testing.T) {
	db, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.NoError(t, err)

	err = db.CreateTable("CREATE TABLE student (age INT, id STRING, isActive BOOL, PRIMARY KEY (id));")
	assert.NoError(t, err)

	err = db.InsertIntoTable("INSERT INTO student VALUES (15, id1234, 1)")

	// todo: tests and code handling cases where data is wrongly provided. eg string provided for INT.
	assert.NoError(t, err)

	res, err := db.SelectFromTable(sqlparser.SelectFromTable{
		TableName:       "student",
		ColumnsRequired: []string{"*"},
		QueryConditions: []sqlparser.QueryCondition{{
			ColumnName: "id",
			QueryType:  "=",
			Value:      "id1234",
		}},
	})
	assert.NoError(t, err)
	assert.Len(t, res, 1)

	assert.Equal(t, []string{"15", "id1234", "1"}, res[0])
}

// create 5 different tables.
// insert few rows in each. do a full table scan of each of them separately and assert result.
func TestSelectFullTableScan(t *testing.T) {
	db, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()
	assert.NoError(t, err)

	tableNames := []string{"customer", "employee", "student", "studentDetails", "teacher"}

	for _, tableName := range tableNames {
		err = db.CreateTable(fmt.Sprintf("CREATE TABLE %s (age INT, id STRING, isActive BOOL, PRIMARY KEY (id));", tableName))
		assert.NoError(t, err)
	}

	j := -1
	for i := 0; i < 100; i++ {
		if i%20 == 0 {
			j++
		}
		err = db.InsertIntoTable(fmt.Sprintf("INSERT INTO %s VALUES (%d, id%d, 1)", tableNames[j], i, i))
		assert.NoError(t, err)
	}

	for i, tableName := range tableNames {
		tableScan, err := db.SelectFromTable(sqlparser.SelectFromTable{
			TableName:       tableName,
			ColumnsRequired: []string{"*"},
		})
		assert.NoError(t, err)
		expectedValStart := (i * 20)
		expectedValEnd := (i * 20) + 19
		assert.Len(t, tableScan, 20)
		for _, row := range tableScan {
			assert.Len(t, row, 3)
			age, err := strconv.Atoi(row[0])
			assert.NoError(t, err)
			assert.GreaterOrEqual(t, age, expectedValStart)
			assert.LessOrEqual(t, age, expectedValEnd)
			assert.Equal(t, row[1], fmt.Sprintf("id%s", row[0]))
			assert.Equal(t, row[2], "1")
		}
	}
	assert.NoError(t, err)
}
