package sqlparser

import (
	"errors"
	"fmt"
)

const (
	KeywordCreate            = "CREATE"
	KeywordTable             = "TABLE"
	KeywordInsert            = "INSERT"
	KeywordInto              = "INTO"
	KeywordValues            = "VALUES"
	KeywordSelect            = "SELECT"
	KeywordFrom              = "FROM"
	KeywordWhere             = "WHERE"
	KeywordAnd               = "AND"
	KeywordPrimary           = "PRIMARY"
	KeywordKey               = "KEY"
	SymbolOpenRoundBracket   = "("
	SymbolClosedRoundBracket = ")"
	SymbolComma              = ","
	SymbolSemiColon          = ";"
	SymbolStar               = "*"
)

const (
	IdentifierColumnName     = "column name"
	IdentifierQueryCondition = "query condition"
	IdentifierQueryValue     = "query value"
)

const (
	ColumnsSelectLimit = 10
)

type Parser struct {
	tokeniser    *Tokeniser
	currentToken Token
}

func NewParser(input string) *Parser {
	t := NewTokeniser(input)
	currentToken := t.NextToken()
	return &Parser{
		tokeniser:    t,
		currentToken: currentToken,
	}
}

// todo: the error messaging needs to be made better such that user doesn't need to go through code
// or any doc.
func (p *Parser) consume(tt TokenType, expectedVal, identifierType string) error {
	if p.currentToken.Type != tt || (expectedVal != "" && p.currentToken.Value != expectedVal) {
		if expectedVal == "" && tt == CONDITIONAL_OPERATOR {
			return fmt.Errorf(
				"syntax error: expected %s, got %s %q",
				tt, p.currentToken.Type, p.currentToken.Value)
		}
		if tt == IDENTIFIER && identifierType != "" {
			if expectedVal == "" {
				return fmt.Errorf(
					"syntax error: expected IDENTIFIER %q, got %s %q",
					identifierType, p.currentToken.Type, p.currentToken.Value)
			}
			return fmt.Errorf(
				"syntax error: expected IDENTIFIER %s %q, got %s %q",
				identifierType, expectedVal, p.currentToken.Type, p.currentToken.Value)
		}
		return fmt.Errorf(
			"syntax error: expected %s %q, got %s %q",
			tt, expectedVal, p.currentToken.Type, p.currentToken.Value)
	}
	p.currentToken = p.tokeniser.NextToken()
	return nil
}

func getDataTypeFromString(columnType string) (DataType, error) {
	switch columnType {
	case "INT":
		return Int, nil
	case "STRING":
		return String, nil
	case "BOOL":
		return Bool, nil
	}
	return DataType(25), fmt.Errorf("data type '%s' not found. expected one of INT, STRING, BOOL",
		columnType)
}

func (p *Parser) parsePrimaryKeyColumn() (string, error) {
	if err := p.consume(KEYWORD, KeywordPrimary, ""); err != nil {
		return "", err
	}
	if err := p.consume(KEYWORD, KeywordKey, ""); err != nil {
		return "", err
	}
	if err := p.consume(SYMBOL, SymbolOpenRoundBracket, ""); err != nil {
		return "", err
	}
	// only primary key with one column supported as of now
	pkColumn := p.currentToken.Value
	if err := p.consume(IDENTIFIER, "", ""); err != nil {
		return "", err
	}
	err := p.consume(SYMBOL, SymbolClosedRoundBracket, "")
	return pkColumn, err
}

func (p *Parser) ParseCreateTable() (*CreateTable, error) {
	if err := p.consume(KEYWORD, KeywordCreate, ""); err != nil {
		return nil, err
	}
	if err := p.consume(KEYWORD, KeywordTable, ""); err != nil {
		return nil, err
	}
	tableName := p.currentToken.Value
	if err := p.consume(IDENTIFIER, "", ""); err != nil {
		return nil, err
	}

	if err := p.consume(SYMBOL, SymbolOpenRoundBracket, ""); err != nil {
		return nil, err
	}

	columnDetails := []Column{}
	pkColumn := ""
	for p.currentToken.Value != SymbolClosedRoundBracket {
		if p.currentToken.Value == "," {
			p.consume(SYMBOL, ",", "")
		}
		if p.currentToken.Value == KeywordPrimary {
			var err error
			pkColumn, err = p.parsePrimaryKeyColumn()
			if err != nil {
				return nil, err
			}
			continue
		}

		columnName := p.currentToken.Value
		if err := p.consume(IDENTIFIER, "", ""); err != nil {
			return nil, err
		}
		columnType := p.currentToken.Value
		if err := p.consume(IDENTIFIER, "", ""); err != nil {
			return nil, err
		}
		dataType, err := getDataTypeFromString(columnType)
		if err != nil {
			return nil, err
		}
		columnDetails = append(columnDetails, Column{
			ColumnName: columnName,
			DataType:   dataType,
		})
	}

	if len(columnDetails) == 0 {
		return nil, fmt.Errorf("expected atleast one column detail, found none")
	}

	pkColumnPosition := 0
	if pkColumn != "" {
		pkColumnPosition = -1
		for i, col := range columnDetails {
			if col.ColumnName == pkColumn {
				pkColumnPosition = i
			}
		}
		if pkColumnPosition == -1 {
			return nil, fmt.Errorf("primary key column '%s' not found", pkColumn)
		}
	}

	if err := p.consume(SYMBOL, SymbolClosedRoundBracket, ""); err != nil {
		return nil, err
	}

	return &CreateTable{
		TableName:                tableName,
		ColumnDetails:            columnDetails,
		PrimaryKeyColumnPosition: pkColumnPosition,
	}, nil
}

// INSERT INTO has 2 syntaxes:
// 1. All column values provided
// INSERT INTO table_name VALUES (all values ...)
// 2. Specific column values provided
// INSERT INTO table_name (col1, col2, col3) VALUES (only the provided column values ...)
// We are supporting only 2 for now.
func (p *Parser) ParseInsertIntoTable() (*InsertIntoTable, error) {
	if err := p.consume(KEYWORD, KeywordInsert, ""); err != nil {
		return nil, err
	}
	if err := p.consume(KEYWORD, KeywordInto, ""); err != nil {
		return nil, err
	}
	tableName := p.currentToken.Value
	if err := p.consume(IDENTIFIER, "", ""); err != nil {
		return nil, err
	}

	if err := p.consume(KEYWORD, KeywordValues, ""); err != nil {
		return nil, err
	}
	if err := p.consume(SYMBOL, SymbolOpenRoundBracket, ""); err != nil {
		return nil, err
	}

	columnValues := []string{}
	for p.currentToken.Value != SymbolClosedRoundBracket {
		if p.currentToken.Value == "," {
			p.consume(SYMBOL, ",", "")
		}

		columnValue := p.currentToken.Value
		if err := p.consume(IDENTIFIER, "", ""); err != nil {
			return nil, err
		}
		columnValues = append(columnValues, columnValue)
	}

	if err := p.consume(SYMBOL, SymbolClosedRoundBracket, ""); err != nil {
		return nil, err
	}

	return &InsertIntoTable{
		TableName:    tableName,
		ColumnValues: columnValues,
	}, nil
}

func (p *Parser) parseColumnsFromSelectQuery() ([]string, error) {
	columnsRequired := []string{}
	for i := 0; p.currentToken.Value != KeywordFrom; i++ {
		if p.currentToken.Value == SymbolComma {
			if err := p.consume(SYMBOL, SymbolComma, ""); err != nil {
				return nil, err
			}
		}
		if i == ColumnsSelectLimit {
			return nil, errors.New("maximum 10 columns supported in SELECT query")
		}
		if p.currentToken.Value == SymbolStar {
			if err := p.consume(SYMBOL, SymbolStar, ""); err != nil {
				return nil, err
			}
			columnsRequired = append(columnsRequired, SymbolStar)
		} else {
			columnName := p.currentToken.Value
			columnsRequired = append(columnsRequired, columnName)
			if err := p.consume(IDENTIFIER, "", ""); err != nil {
				return nil, err
			}
		}
	}
	if len(columnsRequired) == 0 {
		return nil, errors.New("expected atleast 1 column in SELECT query")
	}
	return columnsRequired, nil
}

func (p *Parser) parseQueryConditionsFromSelectQuery() ([]QueryCondition, error) {
	if err := p.consume(KEYWORD, KeywordWhere, ""); err != nil {
		return nil, err
	}
	queryConditions := []QueryCondition{}
	for i := 0; p.currentToken.Value != SymbolSemiColon; i++ {
		if i > 0 {
			if err := p.consume(KEYWORD, KeywordAnd, ""); err != nil {
				return nil, err
			}
		}
		if i == 11 {
			return nil, errors.New("maximum 10 AND conditions supported")
		}

		columnName := p.currentToken.Value
		if err := p.consume(IDENTIFIER, "", IdentifierColumnName); err != nil {
			return nil, err
		}

		queryType := p.currentToken.Value
		if err := p.consume(CONDITIONAL_OPERATOR, "", ""); err != nil {
			return nil, err
		}

		value := p.currentToken.Value
		if err := p.consume(IDENTIFIER, "", IdentifierQueryValue); err != nil {
			return nil, err
		}

		queryConditions = append(queryConditions, QueryCondition{
			ColumnName: columnName,
			QueryType:  QueryType(queryType),
			Value:      value,
		})

		fmt.Printf("queryConditions3333: %+v\n", queryConditions)
	}
	if len(queryConditions) == 0 {
		return nil, errors.New("expected atleast 1 condition within WHERE clause of SELECT query")
	}
	return queryConditions, nil
}

// todo: add a validation before calling Parser. The last character should be ;
func (p *Parser) ParseSelectFromTable() (*SelectFromTable, error) {
	if err := p.consume(KEYWORD, KeywordSelect, ""); err != nil {
		return nil, err
	}
	columnsRequired, err := p.parseColumnsFromSelectQuery()
	if err != nil {
		return nil, err
	}
	if err := p.consume(KEYWORD, KeywordFrom, ""); err != nil {
		return nil, err
	}
	tableName := p.currentToken.Value
	if err := p.consume(IDENTIFIER, "", ""); err != nil {
		return nil, err
	}
	var queryConditions []QueryCondition
	if p.currentToken.Value == KeywordWhere {
		queryConditions, err = p.parseQueryConditionsFromSelectQuery()
		if err != nil {
			return nil, err
		}
	}
	if err := p.consume(SYMBOL, SymbolSemiColon, ""); err != nil {
		return nil, err
	}

	return &SelectFromTable{
		TableName:       tableName,
		ColumnsRequired: columnsRequired,
		QueryConditions: queryConditions,
	}, nil
}
