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

import (
	"seagull.sh/seagull/internal/dialect/ast"
)

// analyseDelete analyses a lightweight `DELETE FROM [db.]table [WHERE ...]` statement.
//
// It marks the analysis as data-modifying (ReadOnly false), records the target table, and
// reuses parseWhereClause so any `{name:Type}` placeholder in the predicate is registered
// as a query parameter. The ALTER TABLE ... DELETE mutation form is a different statement
// kind and is handled by the ALTER TABLE branch.
//
// A RETURNING clause, which ClickHouse does not have, is recorded so the analyser reports
// it.
//
// Returns *ast.RawQueryAnalysis which holds the table and any WHERE parameters.
// Returns error when the DELETE FROM header is malformed.
func (p *parser) analyseDelete() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, p.depthError()
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	analysis := &ast.RawQueryAnalysis{}

	if _, err := p.ExpectKeyword("DELETE"); err != nil {
		return nil, err
	}
	if _, err := p.ExpectKeyword(kwFrom); err != nil {
		return nil, err
	}

	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	analysis.FromTables = []ast.TableReference{{Schema: database, Name: name, Alias: ""}}
	_ = p.matchOnCluster()

	p.parseMutationWhere(analysis)
	p.parseMutationTail(analysis)

	if p.analysisDepth == 1 && p.firstParameterTypeError != nil {
		return analysis, p.firstParameterTypeError
	}
	return analysis, nil
}
