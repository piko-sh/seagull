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

package databasesql

import (
	"go/ast"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/multirow"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

// batchMethod builds a batch or copyfrom INSERT method that repeats VALUES rows within
// the dialect and driver bind limits. Queries with clauses after VALUES run once per row
// to preserve upsert behaviour when keys repeat.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the query's VALUES row cannot be repeated.
func (s *sqlDriver) batchMethod(query *gogen.Query) (ast.Decl, error) {
	spec := &multirow.Spec{
		Bind: func(access ast.Expr, parameter *sqlmodel.QueryParameter, name ast.Expr) ast.Expr {
			access = s.profile.enums.Bound(access, query.Analysed, parameter, query.Types, s.vocabulary)
			if name == nil {
				return s.Bind(access, parameter, query.Imports)
			}
			return s.clickHouseNamed(name, access, parameter, query.Imports)
		},
		Vocabulary: s.vocabulary,
		Connection: sqlConnection,
		Limit:      batchLimit(s.dialect, s.profile),
		PerRow:     query.Analysed.Command == sqlmodel.QueryCommandBatch && emitkit.ClausesAfterValues(query.Analysed, &s.dialect.Lexical),
	}
	method, err := multirow.Method(query, spec)
	if err != nil {
		return nil, err
	}
	return method, nil
}

// batchLimit returns the most arguments one multi-row batch statement binds: the
// dialect's limit, lowered to the driver's own where it has one.
//
// Takes info (*dialect.Facts) which describes the dialect.
// Takes profile (*driverProfile) which is the driver's profile.
//
// Returns int which is the limit.
func batchLimit(info *dialect.Facts, profile *driverProfile) int {
	limit := maxParameters(info)
	if profile.maxBatchParameters > 0 {
		limit = min(limit, profile.maxBatchParameters)
	}
	return limit
}
