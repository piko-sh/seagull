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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

var errProbeRefused = errors.New("the probe dialect does not read this syntax")

type statementText string

func (statementText) IsParsedStatement() {}

func structConstructingDialect(declared ast.ExpressionFeature) dialect.DialectPort {
	return &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Raw: statementText(sql), Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			text, isText := statement.Raw.(statementText)
			if !isText || !strings.Contains(string(text), "{") {
				return nil, fmt.Errorf("%w: %s", errProbeRefused, text)
			}
			constructor := &ast.StructExpression{
				Names:  []string{"a"},
				Values: []ast.Expression{&ast.ColumnRefExpression{TableAlias: "", ColumnName: "id"}},
			}
			return &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: constructor}}}, nil
		},
		SupportedExpressionsFunc: func() ast.ExpressionFeature { return declared },
	}
}

func TestUndeclaredFeaturesLooksInsideAStructConstructor(t *testing.T) {
	t.Parallel()

	window := &ast.WindowFunctionExpression{Function: &ast.FunctionCallExpression{FunctionName: "row_number"}}
	constructor := &ast.StructExpression{
		Names:  []string{"id", "rank"},
		Values: []ast.Expression{&ast.ColumnRefExpression{TableAlias: "", ColumnName: "id"}, window},
	}
	testCases := []struct {
		name     string
		want     []ast.ExpressionFeature
		declared ast.ExpressionFeature
	}{
		{
			name:     "a dialect that declares neither is told of both",
			declared: ast.SQLFeaturesBase,
			want:     []ast.ExpressionFeature{ast.SQLFeatureStructConstruction, ast.SQLFeatureWindowFunction},
		},
		{
			name:     "a dialect that declares the constructor is told of what it holds",
			declared: ast.SQLFeaturesBase | ast.SQLFeatureStructConstruction,
			want:     []ast.ExpressionFeature{ast.SQLFeatureWindowFunction},
		},
		{
			name:     "a dialect that declares both is told of nothing",
			declared: ast.SQLFeaturesBase | ast.SQLFeatureStructConstruction | ast.SQLFeatureWindowFunction,
			want:     nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: constructor}}}
			info := &dialect.Info{Expressions: testCase.declared}

			require.Equal(t, testCase.want, undeclaredFeatures(analysis, info))
		})
	}
}

func TestExpressionKindsFollowInfoChecksStructConstructors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		wantProblems []string
		declared     ast.ExpressionFeature
	}{
		{
			name:         "a dialect that builds a struct it does not declare fails",
			declared:     ast.SQLFeaturesBase,
			wantProblems: []string{`"SELECT {'a': id} FROM items" holds a struct construction ({'a': 1}), which Info.Expressions does not declare`},
		},
		{
			name:         "a dialect that declares the structs it builds passes",
			declared:     ast.SQLFeaturesBase | ast.SQLFeatureStructConstruction,
			wantProblems: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := &Fixture{NewDialect: func() dialect.DialectPort { return structConstructingDialect(testCase.declared) }}
			outcome := checkExpressionKindsFollowInfo(t.Context(), checkEnvironment{testName: t.Name(), inChild: false}, fixture)

			require.Equal(t, testCase.wantProblems, outcome.Problems)
		})
	}
}
