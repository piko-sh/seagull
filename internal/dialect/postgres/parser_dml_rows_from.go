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

package postgres

import (
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// rowsFromLookahead is the offset from ROWS of the parenthesis that opens the function
// list of ROWS FROM (...).
const rowsFromLookahead = 2

// tableFunctionCall is a function call a FROM item reads, as the parser records it before
// it knows whether the item reads one call or several.
type tableFunctionCall struct {
	// qualifiedName is the function's canonical name, optionally schema-qualified.
	qualifiedName string

	// functionName is the function's unqualified canonical name: folded to lower case when
	// unquoted, and as written when quoted.
	functionName string

	// definitions is the call's own column definition list, AS (name type, ...), which a
	// call inside ROWS FROM may give, or nil.
	definitions []ast.TVFColumnDefinition

	// arguments holds the arguments the call passes, in order.
	arguments []ast.Expression
}

// isRowsFromStart reports whether the current position begins a ROWS FROM (...) table
// source, so a table named rows still reads as a table.
//
// Returns bool which is true when ROWS is followed by FROM and an opening parenthesis.
func (p *parser) isRowsFromStart() bool {
	if !p.IsKeyword(keywordROWS) || !p.Peek().IsKeyword(tokenIdentifier, keywordFROM) {
		return false
	}
	return p.Position+rowsFromLookahead < len(p.Tokens) && p.Tokens[p.Position+rowsFromLookahead].Kind == tokenLeftParen
}

// parseRowsFromSource parses a ROWS FROM table source and records the name it is visible
// under.
//
// Takes joinKind (ast.JoinKind) which classifies the join context.
//
// Returns error when the function list is not a parenthesised list of function calls.
func (p *parser) parseRowsFromSource(joinKind ast.JoinKind) error {
	functionAt := len(p.rawTableValuedFunctions)
	if err := p.parseRowsFrom(joinKind); err != nil {
		return err
	}
	if len(p.rawTableValuedFunctions) > functionAt {
		p.recordFromItem(p.rawTableValuedFunctions[functionAt].Alias)
	}
	return nil
}

// parseRowsFrom parses ROWS FROM (f(...) [AS (column definitions)], ...) [WITH
// ORDINALITY] [[AS] alias[(columns)]], whose columns are each call's columns in turn. The
// cursor sits on ROWS.
//
// Takes joinKind (ast.JoinKind) which classifies the join context.
//
// Returns error when the function list is not a parenthesised list of function calls.
func (p *parser) parseRowsFrom(joinKind ast.JoinKind) error {
	p.Advance()
	p.Advance()
	p.Advance()
	var calls []tableFunctionCall
	for {
		if !p.isTableValuedFunctionStart() {
			return p.syntaxError("expected a function call in ROWS FROM")
		}
		call := p.parseTableFunctionCall()
		if p.MatchKeyword(keywordAS) {
			if p.Current().Kind != tokenLeftParen {
				return p.syntaxError("expected '(' to open the column definition list")
			}
			call.definitions = p.parseTVFColumnDefinitions()
		}
		calls = append(calls, call)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return p.syntaxError("expected ')' to close ROWS FROM")
	}
	p.Advance()
	p.recordTableFunctionCalls(calls, joinKind)
	return nil
}

// parseTableFunctionCall parses the name and argument list of a function call in a FROM
// clause, consuming the closing parenthesis. A SQL value function written without
// parentheses, such as CURRENT_DATE, is a call without arguments.
//
// Returns tableFunctionCall which is the call.
func (p *parser) parseTableFunctionCall() tableFunctionCall {
	schemaName, functionName := p.consumeTVFName()
	qualifiedName := functionName
	if schemaName != "" {
		qualifiedName = schemaName + "." + functionName
	}
	if p.Current().Kind != tokenLeftParen {
		return tableFunctionCall{qualifiedName: qualifiedName, functionName: functionName, definitions: nil, arguments: nil}
	}
	p.Advance()
	return tableFunctionCall{
		qualifiedName: qualifiedName,
		functionName:  functionName,
		definitions:   nil,
		arguments:     p.skipTVFArgumentList(qualifiedName),
	}
}

// recordTableFunctionCalls records a FROM item's function calls, ordinality and alias.
//
// Combined calls contribute columns in order, and an alias column list renames those
// columns by position.
//
// Takes calls ([]tableFunctionCall) which holds the item's calls in order.
// Takes joinKind (ast.JoinKind) which classifies the join context.
func (p *parser) recordTableFunctionCalls(calls []tableFunctionCall, joinKind ast.JoinKind) {
	first := calls[0]
	ordinality := p.parseWithOrdinality()
	alias, aliased, columnDefinitions := p.parseTVFAliasAndColumns(first.functionName)
	reference := ast.RawTableValuedFunctionReference{
		FunctionName:      first.qualifiedName,
		Arguments:         nil,
		Alias:             alias,
		ColumnDefinitions: columnDefinitions,
		Ordinality:        ordinality,
		Functions:         nil,
		JoinKind:          joinKind,
	}
	if functions := separateTableFunctionCalls(calls); len(functions) > 1 {
		reference.Functions = functions
	} else {
		reference.Arguments = first.arguments
		reference.ColumnDefinitions = p.oneCallColumns(first, alias, aliased, columnDefinitions)
	}
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, reference)
}

// oneCallColumns returns the column definitions of a FROM item that reads one call: the
// call's own column definition list renamed by the alias column list, or the alias column
// list, or, for a function whose column an alias the item writes names, the alias.
//
// Takes call (tableFunctionCall) which is the item's call.
// Takes alias (string) which is the item's alias.
// Takes aliased (bool) which is true when the item writes an alias.
// Takes aliasColumns ([]ast.TVFColumnDefinition) which is the alias column list, or nil.
//
// Returns []ast.TVFColumnDefinition which holds the item's column definitions, or nil.
func (p *parser) oneCallColumns(
	call tableFunctionCall,
	alias string,
	aliased bool,
	aliasColumns []ast.TVFColumnDefinition,
) []ast.TVFColumnDefinition {
	if call.definitions != nil {
		renamed := slices.Clone(call.definitions)
		for index := range min(len(renamed), len(aliasColumns)) {
			renamed[index].Name = aliasColumns[index].Name
		}
		return renamed
	}
	if aliasColumns == nil && aliased && !p.unnestsARow(call) &&
		p.aliasNamesColumn(call.qualifiedName, call.functionName, len(call.arguments), aliased) {
		return []ast.TVFColumnDefinition{{Name: alias, TypeName: ""}}
	}
	return aliasColumns
}

// unnestsARow reports whether a call of unnest reads a value that is no array, such as
// the TimescaleDB toolkit's timevector its argument's built-in function returns, whose
// unnest gives a row of several columns, so the alias names the row, not a column.
//
// Takes call (tableFunctionCall) which is the call.
//
// Returns bool which is true when the call unnests the result of a built-in function
// every signature of which returns a known type that is no array.
func (p *parser) unnestsARow(call tableFunctionCall) bool {
	if call.functionName != multipleArrayUnnest || len(call.arguments) != 1 || p.functions == nil {
		return false
	}
	argument, isCall := call.arguments[0].(*ast.FunctionCallExpression)
	if !isCall || argument == nil {
		return false
	}
	signatures := p.functions.Functions[argument.FunctionName]
	return len(signatures) > 0 && !slices.ContainsFunc(signatures, func(signature *sqlmodel.Function) bool {
		return signature == nil || !knownType(signature.ReturnType) || signature.ReturnType.Category == sqlmodel.TypeCategoryArray
	})
}

// separateTableFunctionCalls returns the calls whose columns a FROM item reads, one per
// call with its arguments, except that an unnest of several arrays reads as one unnest
// per array, each with its array, as PostgreSQL defines it.
//
// Takes calls ([]tableFunctionCall) which holds the item's calls in order.
//
// Returns []ast.TableFunctionCall which holds the separate calls in order.
func separateTableFunctionCalls(calls []tableFunctionCall) []ast.TableFunctionCall {
	var functions []ast.TableFunctionCall
	for _, call := range calls {
		if call.functionName == multipleArrayUnnest && len(call.arguments) > 1 && call.definitions == nil {
			for _, array := range call.arguments {
				functions = append(functions, ast.TableFunctionCall{
					FunctionName: call.qualifiedName, Arguments: []ast.Expression{array}, ColumnDefinitions: nil,
				})
			}
			continue
		}
		functions = append(functions, ast.TableFunctionCall{
			FunctionName: call.qualifiedName, Arguments: call.arguments, ColumnDefinitions: call.definitions,
		})
	}
	return functions
}
