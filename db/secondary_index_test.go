package db

import (
	"fmt"
	"testing"

	sqlparser "github.com/golang-db/sql_parser"
	"github.com/stretchr/testify/assert"
)

func createTestTable(dbInstance *DB, createIndexOnC2 bool) ([]sqlparser.Column, []sqlparser.SecondaryIndex, error) {
	colDetails := []sqlparser.Column{
		{
			ColumnName: "c1",
			DataType:   sqlparser.String,
		},
		{
			ColumnName: "c2",
			DataType:   sqlparser.String,
		},
		{
			ColumnName: "c3",
			DataType:   sqlparser.Int,
		},
		{
			ColumnName: "c4",
			DataType:   sqlparser.Bool,
		},
	}

	secondaryIndexes := []sqlparser.SecondaryIndex{
		{
			Columns:   []string{"c1"},
			IndexName: "idxc1",
		},
		{
			Columns:   []string{"c3", "c4"},
			IndexName: "idxc3c4",
		},
		// todo: try with incorrect column name as well
	}

	if createIndexOnC2 {
		secondaryIndexes = append(secondaryIndexes, sqlparser.SecondaryIndex{
			Columns:   []string{"c2"},
			IndexName: "idxc2"})
	}

	err := dbInstance.createTable(sqlparser.CreateTable{
		TableName:        "t1",
		ColumnDetails:    colDetails,
		SecondaryIndexes: secondaryIndexes,
	})

	return colDetails, secondaryIndexes, err
}

func TestSecondaryIndexBasedQueries(t *testing.T) {
	dbInstance, cleanupFunc, err := newDBForTest()
	defer cleanupFunc()

	colDetails, secondaryIndexes, err := createTestTable(dbInstance, true)
	assert.NoError(t, err)
	t1Schema := dbInstance.tableNameVsSchemaMap["t1"]
	assert.Equal(t, secondaryIndexes, t1Schema.SecondaryIndexes)
	assert.Equal(t, colDetails, t1Schema.ColumnDetails)

	// spin up a new instance with exactly the same config (wal and ss-table path directories)
	dbInstance2, _, err := newDBForTest()
	assert.NoError(t, err)
	t1Schema = dbInstance2.tableNameVsSchemaMap["t1"]
	assert.Equal(t, secondaryIndexes, t1Schema.SecondaryIndexes)
	assert.Equal(t, colDetails, t1Schema.ColumnDetails)

	for i := 0; i < 10; i++ {
		// todo: we should also support insert with actual data types and not just string
		err = dbInstance2.insertIntoTable(sqlparser.InsertIntoTable{
			TableName: "t1",
			ColumnValues: []string{
				fmt.Sprintf("val1_%d", i),
				fmt.Sprintf("val2_%d", i),
				fmt.Sprintf("%d", i%5),
				fmt.Sprintf("%d", i%2),
			},
		})
		assert.NoError(t, err)
	}

	// part 1: test the result for c1, c2 indexes separately
	for i := 0; i < 20; i++ {
		columnName := "c1"
		valueTemplate := "val1_%d"
		arrIdx := i
		if i >= 10 {
			arrIdx = i - 10
			columnName = "c2"
			valueTemplate = "val2_%d"
		}
		queryRes, err := dbInstance2.selectFromTable(sqlparser.SelectFromTable{
			TableName: "t1",
			QueryConditions: []sqlparser.QueryCondition{
				{
					ColumnName: columnName,
					QueryType:  sqlparser.Equals,
					Value:      fmt.Sprintf(valueTemplate, arrIdx),
				},
			},
		})
		assert.NoError(t, err)
		assert.Len(t, queryRes, 1)
		assert.Equal(t, []string{
			fmt.Sprintf("val1_%d", arrIdx),
			fmt.Sprintf("val2_%d", arrIdx),
			fmt.Sprintf("%d", arrIdx%5), fmt.Sprintf("%d", arrIdx%2)}, queryRes[0])
	}

	// part 2: test the result for c3 and c4 combined composite index
	for i := 0; i < 10; i++ {
		queryRes, err := dbInstance2.selectFromTable(sqlparser.SelectFromTable{
			TableName: "t1",
			QueryConditions: []sqlparser.QueryCondition{
				{
					ColumnName: "c4",
					QueryType:  sqlparser.Equals,
					Value:      fmt.Sprintf("%d", i%2),
				},
				{
					ColumnName: "c3",
					QueryType:  sqlparser.Equals,
					Value:      fmt.Sprintf("%d", i%5),
				},
			},
		})
		assert.NoError(t, err)
		assert.Len(t, queryRes, 1)
		assert.Equal(t, []string{
			fmt.Sprintf("val1_%d", i),
			fmt.Sprintf("val2_%d", i),
			fmt.Sprintf("%d", i%5), fmt.Sprintf("%d", i%2)}, queryRes[0])
	}

	// part 3: test the result for c3 which is a prefix of c3, c4 composite index
	for i := 0; i < 5; i++ {
		queryRes, err := dbInstance2.selectFromTable(sqlparser.SelectFromTable{
			TableName: "t1",
			QueryConditions: []sqlparser.QueryCondition{
				{
					ColumnName: "c3",
					QueryType:  sqlparser.Equals,
					Value:      fmt.Sprintf("%d", i),
				},
			},
		})
		assert.NoError(t, err)
		assert.ElementsMatch(t, [][]string{
			{fmt.Sprintf("val1_%d", i), fmt.Sprintf("val2_%d", i), fmt.Sprintf("%d", i), fmt.Sprintf("%d", i%2)},
			{fmt.Sprintf("val1_%d", i+5), fmt.Sprintf("val2_%d", i+5), fmt.Sprintf("%d", i), fmt.Sprintf("%d", (i+5)%2)}}, queryRes)
	}

	// part 4: test the result for c4: no index exists
	for i := 0; i < 2; i++ {
		queryResult, err := dbInstance2.selectFromTable(sqlparser.SelectFromTable{
			TableName: "t1",
			QueryConditions: []sqlparser.QueryCondition{
				{
					ColumnName: "c4",
					QueryType:  sqlparser.Equals,
					Value:      fmt.Sprintf("%d", i),
				},
			},
		})
		expectedList := [][]string{
			{"val1_0", "val2_0", "0", "0"},
			{"val1_2", "val2_2", "2", "0"},
			{"val1_4", "val2_4", "4", "0"},
			{"val1_6", "val2_6", "1", "0"},
			{"val1_8", "val2_8", "3", "0"},
		}
		if i == 1 {
			expectedList = [][]string{
				{"val1_1", "val2_1", "1", "1"},
				{"val1_3", "val2_3", "3", "1"},
				{"val1_5", "val2_5", "0", "1"},
				{"val1_7", "val2_7", "2", "1"},
				{"val1_9", "val2_9", "4", "1"},
			}
		}
		assert.ElementsMatch(t, expectedList, queryResult)
		assert.NoError(t, err)
	}

	// todo: UTs for filters post index with less than, greater than conditions.
	// todo: logic + UTs for filters via primary key and secondary index with less than, greater than conditions.

	// todo: as of now partial indexes are not supported.
	// Check the result
}
