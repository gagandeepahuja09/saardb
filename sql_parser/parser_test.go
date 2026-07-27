package sqlparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCreateTable(t *testing.T) {
	testCases := []struct {
		name                string
		inputQuery          string
		expectedCreateTable CreateTable
		expectedError       string
	}{
		{
			name:                "Create only",
			inputQuery:          "CREATE",
			expectedCreateTable: CreateTable{},
			expectedError:       "syntax error: expected KEYWORD \"TABLE\", got EOF \"\"",
		},
		{
			name:                "Create database",
			inputQuery:          "CREATE DATABASE",
			expectedCreateTable: CreateTable{},
			expectedError:       "syntax error: expected KEYWORD \"TABLE\", got IDENTIFIER \"DATABASE\"",
		},
		{
			name:                "Create table only",
			inputQuery:          "CREATE TABLE",
			expectedCreateTable: CreateTable{},
			expectedError:       "syntax error: expected IDENTIFIER \"\", got EOF \"\"",
		},
		{
			name:                "Create table with name but no brackets",
			inputQuery:          "CREATE TABLE abc",
			expectedCreateTable: CreateTable{},
			expectedError:       "syntax error: expected SYMBOL \"(\", got EOF \"\"",
		},
		{
			name:                "Create table with name but no brackets",
			inputQuery:          "CREATE TABLE abc ()",
			expectedCreateTable: CreateTable{},
			expectedError:       "expected atleast one column detail, found none",
		},
		{
			name:                "Create table with no data type",
			inputQuery:          "CREATE TABLE abc (something)",
			expectedCreateTable: CreateTable{},
			expectedError:       "syntax error: expected IDENTIFIER \"\", got SYMBOL \")\"",
		},
		{
			name:                "Create table with no data type",
			inputQuery:          "CREATE TABLE abc (something sometype)",
			expectedCreateTable: CreateTable{},
			expectedError:       "data type 'sometype' not found. expected one of INT, STRING, BOOL",
		},
		{
			name:       "Create table with int datatype",
			inputQuery: "CREATE TABLE abc (something INT)",
			expectedCreateTable: CreateTable{
				TableName: "abc",
				ColumnDetails: []Column{{
					ColumnName: "something",
					DataType:   Int,
				}},
			},
			expectedError: "",
		},
		{
			name:       "Create table with multiple datatypes",
			inputQuery: "CREATE TABLE abc (someNum INT, someStr STRING, someBool BOOL)",
			expectedCreateTable: CreateTable{
				TableName: "abc",
				ColumnDetails: []Column{{
					ColumnName: "someNum",
					DataType:   Int,
				},
					{
						ColumnName: "someStr",
						DataType:   String,
					},
					{
						ColumnName: "someBool",
						DataType:   Bool,
					}},
			},
			expectedError: "",
		},
		{
			name:       "Create table with multiple datatypes and primary key as the first column",
			inputQuery: "CREATE TABLE abc (someNum INT, someStr STRING, someBool BOOL, PRIMARY KEY (someNum))",
			expectedCreateTable: CreateTable{
				TableName: "abc",
				ColumnDetails: []Column{{
					ColumnName: "someNum",
					DataType:   Int,
				},
					{
						ColumnName: "someStr",
						DataType:   String,
					},
					{
						ColumnName: "someBool",
						DataType:   Bool,
					}},
			},
			expectedError: "",
		},
		{
			name:       "Create table with multiple datatypes and primary key column doesn't exist",
			inputQuery: "CREATE TABLE abc (someNum INT, someStr STRING, someBool BOOL, PRIMARY KEY (someOtherNum))",
			expectedCreateTable: CreateTable{
				TableName: "abc",
				ColumnDetails: []Column{{
					ColumnName: "someNum",
					DataType:   Int,
				},
					{
						ColumnName: "someStr",
						DataType:   String,
					},
					{
						ColumnName: "someBool",
						DataType:   Bool,
					}},
			},
			expectedError: "primary key column 'someOtherNum' not found",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.inputQuery)
			input, err := parser.ParseCreateTable()
			if err != nil {
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				assert.Equal(t, tt.expectedCreateTable, *input)
			}
		})
	}
}

func TestParseInsertIntoTable(t *testing.T) {
	testCases := []struct {
		name                    string
		inputQuery              string
		expectedInsertIntoTable InsertIntoTable
		expectedError           string
	}{
		{
			name:                    "Insert into only",
			inputQuery:              "INSERT INTO",
			expectedInsertIntoTable: InsertIntoTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"\", got EOF \"\"",
		},
		{
			name:                    "Insert with table name but no VALUES",
			inputQuery:              "INSERT INTO payments ()",
			expectedInsertIntoTable: InsertIntoTable{},
			expectedError:           "syntax error: expected KEYWORD \"VALUES\", got SYMBOL \"(\"",
		},
		{
			name:       "Insert with column values",
			inputQuery: "INSERT INTO payments VALUES (1234, age, 0)",
			expectedInsertIntoTable: InsertIntoTable{
				TableName:    "payments",
				ColumnValues: []string{"1234", "age", "0"},
			},
			expectedError: "",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.inputQuery)
			input, err := parser.ParseInsertIntoTable()
			if err != nil {
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				assert.Equal(t, tt.expectedInsertIntoTable, *input)
			}
		})
	}
}

func TestParseSelectFromTable(t *testing.T) {
	testCases := []struct {
		name                    string
		inputQuery              string
		expectedSelectFromTable SelectFromTable
		expectedError           string
	}{
		{
			name:                    "Only select",
			inputQuery:              "SELECT;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"\", got SYMBOL \";\"",
		},
		{
			name:                    "Select with column name",
			inputQuery:              "SELECT c1;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"\", got SYMBOL \";\"",
		},
		{
			name:                    "Select column name and WHERE",
			inputQuery:              "SELECT c1 WHERE;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"\", got KEYWORD \"WHERE\"",
		},
		{
			name:                    "select column name and from",
			inputQuery:              "SELECT c1 FROM;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"\", got SYMBOL \";\"",
		},
		{
			name:                    "Select 11 columns",
			inputQuery:              "SELECT c1, c2, c3, c4, c5, c6, c7, c8, c9, c10, c11 FROM table1;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "maximum 10 columns supported in SELECT query",
		},
		{
			name:       "Select 10 columns",
			inputQuery: "SELECT c1, c2, c3, c4, c5, c6, c7, c8, c9, c10 FROM table1;",
			expectedSelectFromTable: SelectFromTable{
				TableName:       "table1",
				ColumnsRequired: []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10"},
			},
			expectedError: "maximum 10 columns supported in SELECT query",
		},
		{
			name:                    "No Column selected",
			inputQuery:              "SELECT FROM table1;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "expected atleast 1 column in SELECT query",
		},
		{
			name:       "Select correct query without WHERE clause",
			inputQuery: "SELECT * FROM students;",
			expectedSelectFromTable: SelectFromTable{
				TableName:       "students",
				ColumnsRequired: []string{"*"},
				QueryConditions: nil,
			},
			expectedError: "",
		},
		{
			name:                    "Select query AND before WHERE",
			inputQuery:              "SELECT * FROM students AND;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected SYMBOL \";\", got KEYWORD \"AND\"",
		},
		{
			name:                    "Select with WHERE clause but no condition",
			inputQuery:              "SELECT * FROM students WHERE;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "expected atleast 1 condition within WHERE clause of SELECT query",
		},
		{
			name:                    "Select with WHERE clause but condition only having column name",
			inputQuery:              "SELECT * FROM students WHERE name;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected CONDITIONAL_OPERATOR, got SYMBOL \";\"",
		},
		{
			name:                    "Select with WHERE clause but condition not having expected column conditional operator",
			inputQuery:              "SELECT * FROM students WHERE name IS;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected CONDITIONAL_OPERATOR, got IDENTIFIER \"IS\"",
		},
		{
			name:                    "Select with WHERE clause but condition not having column value",
			inputQuery:              "SELECT * FROM students WHERE name =;",
			expectedSelectFromTable: SelectFromTable{},
			expectedError:           "syntax error: expected IDENTIFIER \"query value\", got SYMBOL \";\"",
		},
		{
			// todo: we need to support multi-word string like: WHERE name = 'Gagandeep Singh Ahuja'
			name:       "Select with WHERE clause and complete condition",
			inputQuery: "SELECT * FROM students WHERE name = Gagan;",
			expectedSelectFromTable: SelectFromTable{
				TableName:       "students",
				ColumnsRequired: []string{"*"},
				QueryConditions: []QueryCondition{{
					ColumnName: "name",
					Value:      "Gagan",
					QueryType:  "=",
				},
				},
			},
			expectedError: "",
		},
		{
			name:       "Select with WHERE clause and complete condition",
			inputQuery: "SELECT * FROM students WHERE name == Gagan;",
			expectedSelectFromTable: SelectFromTable{
				TableName:       "students",
				ColumnsRequired: []string{"*"},
				QueryConditions: []QueryCondition{{
					ColumnName: "name",
					Value:      "Gagan",
					// todo: this should not be supported
					QueryType: "==",
				},
				},
			},
			expectedError: "",
		},
		// todo: tests for AND condition
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.inputQuery)
			input, err := parser.ParseSelectFromTable()
			if err != nil {
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				assert.Equal(t, tt.expectedSelectFromTable, *input)
			}
		})
	}
}
