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

import "seagull.sh/seagull/internal/dialect/ast"

// declaredIndex is a FULLTEXT or other non-unique index a table definition declares.
type declaredIndex struct {
	// name is the index's name, for a FULLTEXT index the first column's when the definition
	// gives none, and otherwise empty then.
	name string

	// columns are the index's columns, in order.
	columns []string

	// fullText is true for a FULLTEXT index.
	fullText bool
}

// queryDetail is what the MySQL dialect records about a query beyond the neutral
// analysis: its full-text searches, which a check compares with the FULLTEXT indexes.
type queryDetail struct {
	// fullTextSearches are the query's MATCH ... AGAINST searches.
	fullTextSearches []ast.FullTextSearch
}

// EngineDetail marks queryDetail as the dialect's detail.
func (*queryDetail) EngineDetail() {}

// FullTextSearches returns the query's full-text searches.
//
// Returns []ast.FullTextSearch which holds them in the order the query writes them.
func (detail *queryDetail) FullTextSearches() []ast.FullTextSearch {
	return detail.fullTextSearches
}

// fullTextSearchOf reads the column list of a MATCH (...) clause.
//
// A search naming anything other than plain columns is left out because no index can
// match it.
//
// Takes expressions ([]ast.Expression) which are the parsed list.
//
// Returns ast.FullTextSearch which is the search.
// Returns bool which is false when an item is not a column.
func fullTextSearchOf(expressions []ast.Expression) (ast.FullTextSearch, bool) {
	search := ast.FullTextSearch{Query: nil, Columns: make([]ast.ColumnReference, 0, len(expressions))}
	for _, expression := range expressions {
		column, isColumn := expression.(*ast.ColumnRefExpression)
		if !isColumn {
			return ast.FullTextSearch{Query: nil, Columns: nil}, false
		}
		search.Columns = append(search.Columns, ast.ColumnReference{TableAlias: column.TableAlias, ColumnName: column.ColumnName})
	}
	return search, len(search.Columns) > 0
}
