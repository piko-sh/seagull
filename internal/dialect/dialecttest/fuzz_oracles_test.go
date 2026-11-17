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

package dialecttest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

type fuzzPanicDialect struct{ dialect.DialectPort }

func (fuzzPanicDialect) AnalyseQuery(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
	panic("injected fuzz oracle panic")
}

func TestRawGenerationOracleRejectsRecoveredPanics(t *testing.T) {
	engine := fuzzPanicDialect{DialectPort: postgres.New()}
	problems, err := generateInvariantProblems(t.Context(), engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(FuzzQuery, many)\nSELECT id FROM items;", time.Second)
	require.NoError(t, err)
	require.Contains(t, strings.Join(problems, "\n"), "generation recovered a panic")
}

func TestPlaceholderOracleRejectsAllMissingSpans(t *testing.T) {
	info := postgres.New().Info()
	tokens, err := significantTokens(info.Lexical, "SELECT $1")
	require.NoError(t, err)
	subject := spanSubject{analysis: &ast.RawQueryAnalysis{}, text: "SELECT $1", source: "SELECT $1", tokens: tokens}
	require.NotEmpty(t, placeholderSpanProblems(&spanCorpus{info: info}, subject))
}
