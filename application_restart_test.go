package main

import (
	"testing"

	"github.com/golang-db/db"
	sqlparser "github.com/golang-db/sql_parser"
	"github.com/stretchr/testify/assert"
)

var testPaymentsTable = sqlparser.CreateTable{
	TableName: "payments",
	ColumnDetails: []sqlparser.Column{
		{
			ColumnName: "id",
			DataType:   sqlparser.Int,
		},
		{
			ColumnName: "status",
			DataType:   sqlparser.String,
		},
		{
			ColumnName: "international",
			DataType:   sqlparser.Bool,
		},
	},
	SecondaryIndexes: []sqlparser.SecondaryIndex{},
}

var testRefundsTable = sqlparser.CreateTable{
	TableName:                "refunds",
	PrimaryKeyColumnPosition: 1,
	ColumnDetails: []sqlparser.Column{
		{
			ColumnName: "status",
			DataType:   1,
		},
		{
			ColumnName: "id",
			DataType:   0,
		},
	},
	SecondaryIndexes: []sqlparser.SecondaryIndex{},
}

func TestAppRestart(t *testing.T) {
	defer dbDirCleanUp(t)
	// what happens if tests run in parallel?
	dbForPut, err := db.NewDB(testDbConfig)
	assert.NoError(t, err)

	dbForPut.CreateTable("CREATE TABLE payments (id INT, status STRING, international BOOL)")
	dbForPut.CreateTable("CREATE TABLE refunds (status STRING, id INT, PRIMARY KEY (id))")
	buildTestData(dbForPut)

	// new instance created to test for app restart
	// creating a separate instance is similar to testing for app restart
	dbForGet, err := db.NewDB(testDbConfig)
	assert.NoError(t, err)
	tableNames := dbForGet.ShowTables()
	assert.Equal(t, []string{"payments", "refunds"}, tableNames)

	paymentsTable, err := dbForGet.ShowCreateTable("payments")
	assert.NoError(t, err)
	assert.Equal(t, testPaymentsTable, *paymentsTable)

	refundsTable, err := dbForGet.ShowCreateTable("refunds")
	assert.NoError(t, err)
	assert.Equal(t, testRefundsTable, *refundsTable)

	txnTable, err := dbForGet.ShowCreateTable("transactions")
	assert.Equal(t, "table: 'transactions' not found", err.Error())
	assert.Nil(t, txnTable)

	assertValuesForTestData(t, dbForGet)
}
