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

package duckdb

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ordinalityColumnName is the name DuckDB gives the column WITH ORDINALITY appends after
// a table function's columns, unless an alias column list names it.
const ordinalityColumnName = "ordinality"

var (
	// tableValuedFunctionColumns describes the row shapes returned by well-known DuckDB
	// table-valued functions. A nil entry marks a file-dependent shape that this static
	// catalogue does not resolve.
	tableValuedFunctionColumns = map[string][]ast.ScopedColumn{
		"generate_series": {
			{
				Name:     "generate_series",
				SQLType:  builtinTypeMap["bigint"],
				Nullable: false,
			},
		},
		"range": {
			{Name: "range", SQLType: builtinTypeMap["bigint"], Nullable: false},
		},
		"unnest": {
			{Name: "unnest", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
		},
		"regexp_matches": {
			{
				Name:     "regexp_matches",
				SQLType:  newArrayType(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")),
				Nullable: true,
			},
		},
		"read_parquet":   nil,
		"read_csv":       nil,
		"read_csv_auto":  nil,
		"read_json":      nil,
		"read_json_auto": nil,
	}
)
