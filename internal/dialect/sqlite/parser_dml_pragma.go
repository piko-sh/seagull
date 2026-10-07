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

package sqlite

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// pragmaResultPrefix starts the name a PRAGMA statement's analysis reads its rows under,
	// as a table function. It holds a space, so no function a query calls in a FROM clause
	// can share the name.
	pragmaResultPrefix = "pragma "

	// pragmaFunctionPrefix starts the name of the table function that reads a pragma, such
	// as pragma_table_info.
	pragmaFunctionPrefix = "pragma_"
)

var (
	// pragmaStatementColumns maps the pragmas whose reading only a PRAGMA statement gives,
	// beyond those that also have a table function, to the columns the statement returns, as
	// SQLite 3.51 and 3.53 give them.
	pragmaStatementColumns = map[string][]ast.ScopedColumn{
		"analysis_limit":            {integerColumn("analysis_limit", false)},
		"auto_vacuum":               {integerColumn("auto_vacuum", false)},
		"automatic_index":           {integerColumn("automatic_index", false)},
		"busy_timeout":              {integerColumn("timeout", false)},
		"cache_size":                {integerColumn("cache_size", false)},
		"cache_spill":               {integerColumn("cache_spill", false)},
		"cell_size_check":           {integerColumn("cell_size_check", false)},
		"checkpoint_fullfsync":      {integerColumn("checkpoint_fullfsync", false)},
		"data_version":              {integerColumn("data_version", false)},
		"defer_foreign_keys":        {integerColumn("defer_foreign_keys", false)},
		"fullfsync":                 {integerColumn("fullfsync", false)},
		"hard_heap_limit":           {integerColumn("hard_heap_limit", false)},
		"ignore_check_constraints":  {integerColumn("ignore_check_constraints", false)},
		"journal_size_limit":        {integerColumn("journal_size_limit", false)},
		"legacy_alter_table":        {integerColumn("legacy_alter_table", false)},
		"locking_mode":              {textColumn("locking_mode", false)},
		"max_page_count":            {integerColumn("max_page_count", false)},
		"mmap_size":                 {integerColumn("mmap_size", false)},
		"query_only":                {integerColumn("query_only", false)},
		"read_uncommitted":          {integerColumn("read_uncommitted", false)},
		"recursive_triggers":        {integerColumn("recursive_triggers", false)},
		"reverse_unordered_selects": {integerColumn("reverse_unordered_selects", false)},
		"secure_delete":             {integerColumn("secure_delete", false)},
		"soft_heap_limit":           {integerColumn("soft_heap_limit", false)},
		"synchronous":               {integerColumn("synchronous", false)},
		"temp_store":                {integerColumn("temp_store", false)},
		"threads":                   {integerColumn("threads", false)},
		"trusted_schema":            {integerColumn("trusted_schema", false)},
		"wal_autocheckpoint":        {integerColumn("wal_autocheckpoint", false)},
		"wal_checkpoint":            {integerColumn("busy", false), integerColumn("log", false), integerColumn("checkpointed", false)},
		"writable_schema":           {integerColumn("writable_schema", false)},
	}

	// pragmaResults maps every pragma whose result the dialect declares, by the name a
	// PRAGMA statement's analysis reads its rows under, to the columns it returns.
	pragmaResults = declaredPragmaResults()

	// pragmasTakingArguments lists the pragmas whose value in parentheses, or after =, is an
	// argument of what they read, such as the table table_info describes, rather than a
	// setting they change.
	pragmasTakingArguments = map[string]bool{
		"table_info": true, "table_xinfo": true, "table_list": true, "index_list": true, "index_info": true,
		"index_xinfo": true, "foreign_key_list": true, "foreign_key_check": true, "integrity_check": true,
		"quick_check": true, "wal_checkpoint": true,
	}

	// pragmasEchoingSettings lists the pragmas that return the setting they change, as
	// SQLite gives it after the change.
	pragmasEchoingSettings = map[string]bool{
		"analysis_limit": true, "busy_timeout": true, "hard_heap_limit": true, "journal_mode": true,
		"journal_size_limit": true, "locking_mode": true, "max_page_count": true, "mmap_size": true,
		"secure_delete": true, "soft_heap_limit": true, "threads": true, "wal_autocheckpoint": true,
	}

	// undeclaredPragmas lists the pragmas that return rows whose columns the dialect does
	// not declare: deprecated ones, which a build may leave out, and the debugging ones.
	undeclaredPragmas = map[string]bool{
		"count_changes": true, "default_cache_size": true, "empty_result_callbacks": true,
		"full_column_names": true, "short_column_names": true, "temp_store_directory": true,
		"data_store_directory": true, "stats": true, "parser_trace": true, "vdbe_addoptrace": true,
		"vdbe_debug": true, "vdbe_listing": true, "vdbe_trace": true,
	}
)

// analysePragma types declared PRAGMA results as table-function columns and returns no
// rows for settings or unknown pragmas. It rejects parameters, malformed statements and
// reads with undeclared result columns; PRAGMA analyses are never read-only.
//
// Returns *ast.RawQueryAnalysis which reads the statement's result, if any.
// Returns error which is a dialectkit.ParseError for a parameter, a pragma whose rows are
// not declared, or a malformed statement.
func (p *parser) analysePragma() (*ast.RawQueryAnalysis, error) {
	for _, candidate := range p.Tokens {
		if isParameterToken(candidate.Kind) {
			return nil, dialectkit.NewParseError(candidate, "a PRAGMA statement cannot bind a parameter")
		}
	}
	if _, err := p.ExpectKeyword("PRAGMA"); err != nil {
		return nil, err
	}
	nameToken := p.Current()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	if p.Current().Kind == tokenDot {
		p.Advance()
		nameToken = p.Current()
		if name, err = p.parseIdentifierOrKeyword(); err != nil {
			return nil, err
		}
	}
	lowered := strings.ToLower(name)
	valued, err := p.skipPragmaValue()
	if err != nil {
		return nil, err
	}
	analysis := &ast.RawQueryAnalysis{}
	resultName := pragmaResultPrefix + lowered
	if _, declared := pragmaResults[resultName]; !declared || (valued && !pragmasTakingArguments[lowered] && !pragmasEchoingSettings[lowered]) {
		if !valued && undeclaredPragmas[lowered] {
			return nil, dialectkit.NewParseError(nameToken, "the columns PRAGMA "+lowered+
				" returns are not declared; it is deprecated or for debugging, so read the setting another way")
		}
		return analysis, nil
	}
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
	analysis.RawTableValuedFunctions = []ast.RawTableValuedFunctionReference{{
		FunctionName:      resultName,
		Arguments:         nil,
		Alias:             "",
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
		JoinKind:          ast.JoinInner,
	}}
	return analysis, nil
}

// skipPragmaValue consumes the value of a PRAGMA statement, after = or in parentheses,
// when it has one.
//
// Returns bool which is true when the statement gives a value.
// Returns error when the parentheses do not close.
func (p *parser) skipPragmaValue() (bool, error) {
	switch {
	case p.Current().Kind == tokenLeftParen:
		return true, p.skipParenthesised()
	case p.Current().Kind == tokenOperator && p.Current().Value == "=":
		p.Advance()
		if p.Current().Kind == tokenOperator && (p.Current().Value == "-" || p.Current().Value == "+") {
			p.Advance()
		}
		if !p.AtEnd() {
			p.Advance()
		}
		return true, nil
	default:
		return false, nil
	}
}

// declaredPragmaResults builds the declared pragma results: those of every pragma with a
// table function and those only a PRAGMA statement gives.
//
// Returns map[string][]ast.ScopedColumn which maps the name a PRAGMA statement's analysis
// reads each pragma's rows under to its columns.
func declaredPragmaResults() map[string][]ast.ScopedColumn {
	results := make(map[string][]ast.ScopedColumn, len(pragmaFunctionColumns)+len(pragmaStatementColumns))
	for name, columns := range pragmaFunctionColumns {
		results[pragmaResultPrefix+strings.TrimPrefix(name, pragmaFunctionPrefix)] = columns
	}
	for name, columns := range pragmaStatementColumns {
		results[pragmaResultPrefix+name] = columns
	}
	return results
}
