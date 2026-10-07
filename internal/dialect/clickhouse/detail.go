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

package clickhouse

import "seagull.sh/seagull/internal/dialect/ast"

// queryDetail is what the ClickHouse dialect records about a query beyond the neutral
// analysis: the options of clauses only ClickHouse has. The analyser never reads it.
type queryDetail struct {
	// groupingSets is the text of a GROUP BY GROUPING SETS body.
	groupingSets string

	// limitFillFrom is the text of the FROM bound of a LIMIT ... WITH FILL clause.
	limitFillFrom string

	// limitFillTo is the text of the TO bound of a LIMIT ... WITH FILL clause.
	limitFillTo string

	// limitFillStep is the text of the STEP of a LIMIT ... WITH FILL clause.
	limitFillStep string

	// asyncBody is the action body of an asynchronous ALTER TABLE ... UPDATE or DELETE
	// mutation.
	asyncBody string

	// expressionAliases lists the names the query's WITH clause gives expressions, such as
	// bound in WITH now() - 7 AS bound, which its clauses and subqueries may use as values.
	expressionAliases []string

	// groupByAll reports a GROUP BY ALL clause.
	groupByAll bool

	// qualify reports a QUALIFY clause.
	qualify bool
}

// EngineDetail marks queryDetail as the dialect's detail.
func (*queryDetail) EngineDetail() {}

// cteDetail is what the ClickHouse dialect records about a common table expression beyond
// its neutral definition: its MATERIALIZED or NOT MATERIALIZED qualifier.
type cteDetail struct {
	// materialized is true for MATERIALIZED and false for NOT MATERIALIZED.
	materialized bool
}

// EngineDetail marks cteDetail as the dialect's detail.
func (*cteDetail) EngineDetail() {}

// detailOf returns the dialect's detail of an analysis, attaching an empty one first when
// the analysis has none.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis.
//
// Returns *queryDetail which is the analysis's detail.
func detailOf(analysis *ast.RawQueryAnalysis) *queryDetail {
	if detail, found := analysis.Detail.(*queryDetail); found {
		return detail
	}
	detail := &queryDetail{}
	analysis.Detail = detail
	return detail
}
