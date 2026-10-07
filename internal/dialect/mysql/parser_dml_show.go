// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package mysql

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// tablesColumnPrefix starts the name of the column SHOW TABLES gives, which the server
	// completes with the name of the database it lists.
	tablesColumnPrefix = "Tables_in_"
)

// showColumn is a column a SHOW statement returns.
type showColumn struct {
	// name is the column's name as the server gives it.
	name string

	// typeName is the column's type, as the dialect's type table spells it.
	typeName string

	// nullable reports a column that may be NULL on MySQL or MariaDB.
	nullable bool
}

var (
	// diagnosticsColumns are the columns of SHOW WARNINGS and SHOW ERRORS.
	diagnosticsColumns = []showColumn{
		{name: "Level", typeName: "varchar", nullable: false},
		{name: "Code", typeName: "int unsigned", nullable: false},
		{name: "Message", typeName: "varchar", nullable: false},
	}

	// settingColumns are the columns of SHOW VARIABLES and SHOW STATUS. MySQL may give a
	// variable no value.
	settingColumns = []showColumn{
		{name: "Variable_name", typeName: "varchar", nullable: false},
		{name: "Value", typeName: "varchar", nullable: true},
	}

	// showResults maps each SHOW statement whose result the dialect declares, by the name
	// its analysis reads the rows under, to the columns it returns, as MySQL 8.4 and MariaDB
	// 11 give them. The names hold a space, so no function a query calls in a FROM clause
	// can share one.
	showResults = map[string][]showColumn{
		"show warnings":       diagnosticsColumns,
		"show errors":         diagnosticsColumns,
		"show count warnings": {{name: "@@session.warning_count", typeName: "bigint unsigned", nullable: true}},
		"show count errors":   {{name: "@@session.error_count", typeName: "bigint unsigned", nullable: true}},
		"show variables":      settingColumns,
		"show status":         settingColumns,
		"show databases":      {{name: "Database", typeName: "varchar", nullable: false}},
		"show create table":   {{name: "Table", typeName: "varchar", nullable: false}, {name: "Create Table", typeName: "varchar", nullable: false}},
		"show tables":         {{name: tablesColumnPrefix + "database", typeName: "varchar", nullable: false}},
		"show full tables": {
			{name: tablesColumnPrefix + "database", typeName: "varchar", nullable: false},
			{name: "Table_type", typeName: "varchar", nullable: false},
		},
	}

	// showScopes are the words that may stand before VARIABLES or STATUS to say whose
	// settings the statement shows.
	showScopes = []string{"GLOBAL", "SESSION", "LOCAL"}
)

// analyseShow analyses supported SHOW statements as table-function results so the
// analyser can type their columns. It rejects parameters and SHOW forms whose result
// columns are not declared.
//
// Returns *ast.RawQueryAnalysis which reads the statement's result.
// Returns error which is a dialectkit.ParseError for a SHOW statement whose result is not
// declared or that holds a parameter.
func (p *parser) analyseShow() (*ast.RawQueryAnalysis, error) {
	for _, candidate := range p.Tokens {
		if isParameterToken(candidate.Kind) {
			return nil, dialectkit.NewParseError(candidate, "a SHOW statement cannot bind a parameter")
		}
	}
	if _, err := p.ExpectKeyword("SHOW"); err != nil {
		return nil, err
	}
	name, declared := p.showResultName()
	if !declared {
		return nil, dialectkit.NewParseError(p.Current(), "the columns SHOW "+strings.ToUpper(p.Current().Raw)+
			" returns are not declared; read them from information_schema, or use SHOW WARNINGS, ERRORS, VARIABLES, "+
			"STATUS, DATABASES, TABLES or CREATE TABLE")
	}
	columnNames := p.shownTablesColumnNames(name)
	p.Position = len(p.Tokens)
	analysis := &ast.RawQueryAnalysis{}
	analysis.ReadOnly = true
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      name,
		Arguments:         nil,
		Alias:             "",
		ColumnDefinitions: columnNames,
		Ordinality:        nil,
		Functions:         nil,
		JoinKind:          ast.JoinInner,
	})
	p.finaliseAnalysis(analysis)
	return analysis, nil
}

// showResultName reads the words after SHOW that name what the statement shows and
// returns the name showResults keys its columns by.
//
// Returns string which is the name.
// Returns bool which is false when the dialect does not declare the statement's result.
func (p *parser) showResultName() (string, bool) {
	switch {
	case p.MatchKeyword("WARNINGS"):
		return "show warnings", true
	case p.MatchKeyword("ERRORS"):
		return "show errors", true
	case p.IsKeyword("COUNT") && p.Peek().Kind == tokenLeftParen:
		return p.showCountResultName()
	case p.MatchKeyword("DATABASES"), p.MatchKeyword("SCHEMAS"):
		return "show databases", true
	case p.IsKeyword(keywordCREATE) && p.Peek().IsKeyword(tokenIdentifier, keywordTABLE):
		return "show create table", true
	}
	if p.IsKeyword(showScopes...) {
		p.Advance()
	}
	switch {
	case p.MatchKeyword("VARIABLES"):
		return "show variables", true
	case p.MatchKeyword("STATUS"):
		return "show status", true
	case p.MatchKeyword("TABLES"):
		return "show tables", true
	case p.IsKeyword("FULL") && p.Peek().IsKeyword(tokenIdentifier, "TABLES"):
		p.Advance()
		p.Advance()
		return "show full tables", true
	default:
		return "", false
	}
}

// shownTablesColumnNames names the first column of SHOW TABLES FROM or IN a database
// after the database, as the server does; any other statement keeps its declared names.
//
// Takes name (string) which is the name showResults keys the statement's columns by.
//
// Returns []ast.TVFColumnDefinition which renames the first column, or nil.
func (p *parser) shownTablesColumnNames(name string) []ast.TVFColumnDefinition {
	if (name != "show tables" && name != "show full tables") || !p.IsKeyword(keywordFROM, "IN") ||
		p.Peek().Kind != tokenIdentifier {
		return nil
	}
	return []ast.TVFColumnDefinition{{Name: tablesColumnPrefix + p.Peek().Value, TypeName: ""}}
}

// showCountResultName reads COUNT(*) WARNINGS or COUNT(*) ERRORS.
//
// Returns string which is the name showResults keys the count by.
// Returns bool which is false when COUNT(*) is not followed by WARNINGS or ERRORS.
func (p *parser) showCountResultName() (string, bool) {
	p.Advance()
	if err := p.skipParenthesised(); err != nil {
		return "", false
	}
	switch {
	case p.MatchKeyword("WARNINGS"):
		return "show count warnings", true
	case p.MatchKeyword("ERRORS"):
		return "show count errors", true
	default:
		return "", false
	}
}
