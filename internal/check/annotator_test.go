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

package check

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestDiagnosticAnalyserRunsNoPassOnceTheContextIsDone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		cancelled bool
		wantNone  bool
	}{
		{name: "a live context runs every pass", cancelled: false, wantNone: false},
		{name: "a cancelled context runs none", cancelled: true, wantNone: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if testCase.cancelled {
				cancel()
			}
			subject := &DiagnosticContext{
				Filename:            "test.sql",
				Query:               &sqlmodel.AnalysedQuery{Name: "Broken", Command: sqlmodel.QueryCommandOne, Line: 10},
				RawAnalysis:         &ast.RawQueryAnalysis{},
				ParameterDirectives: []*directive.ParameterDirective{{Number: 1, Name: "unused", Kind: sqlmodel.ParameterDirectiveParam}},
			}

			diagnostics := NewDiagnosticAnalyser(&fakedialect.Engine{}, fakedialect.NewCatalogue("main")).Analyse(ctx, subject)

			require.Equal(t, testCase.wantNone, len(diagnostics) == 0)
		})
	}
}

func TestDiagnosticAnalyser_Analyse(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("main")
	analyser := NewDiagnosticAnalyser(&fakedialect.Engine{}, catalogue)

	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:          "BrokenQuery",
			Command:       sqlmodel.QueryCommandOne,
			OutputColumns: nil,
			Line:          10,
		},
		RawAnalysis: &ast.RawQueryAnalysis{
			ParameterReferences: []ast.RawParameterReference{},
		},
		ParameterDirectives: []*directive.ParameterDirective{
			{Number: 1, Name: "unused_param", Kind: sqlmodel.ParameterDirectiveParam},
		},
	}

	diagnostics := analyser.Analyse(t.Context(), subject)

	assert.GreaterOrEqual(t, len(diagnostics), 2,
		"analyser should collect diagnostics from multiple passes")

	codeSet := make(map[string]int)
	for _, diagnostic := range diagnostics {
		codeSet[diagnostic.Code]++
	}

	assert.GreaterOrEqual(t, codeSet["Q009"], 1,
		"command/output mismatch should emit Q009")
	assert.GreaterOrEqual(t, codeSet[diag.CodeUnreferencedParameter], 1,
		"unreferenced parameter should emit its dedicated code")
}

func TestDiagnosticAnalyser_NoDiagnostics(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("main")
	analyser := NewDiagnosticAnalyser(&fakedialect.Engine{}, catalogue)

	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "GoodQuery",
			Command: sqlmodel.QueryCommandMany,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
				{Name: "email"},
			},
			Line: 1,
		},
		RawAnalysis: &ast.RawQueryAnalysis{
			ParameterReferences: []ast.RawParameterReference{
				{Number: 1},
			},
		},
		ParameterDirectives: []*directive.ParameterDirective{
			{Number: 1, Name: "user_id", Kind: sqlmodel.ParameterDirectiveParam},
		},
	}

	diagnostics := analyser.Analyse(t.Context(), subject)
	assert.Empty(t, diagnostics)
}

func TestNewDiagnosticAnalyserReadsTheDialectOnlyWhenItIsMade(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries int
	}{
		{name: "one query", queries: 1},
		{name: "many queries", queries: 25},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reads := 0
			info := (&fakedialect.Engine{}).Info()
			engine := &fakedialect.Engine{InfoFunc: func() dialect.Info {
				reads++
				return info
			}}
			rules := resolve.ScopeRulesFor(engine)
			analyser := NewDiagnosticAnalyser(engine, referenceCatalogue())
			readsWhenMade := reads

			for range testCase.queries {
				raw := itemsQuery(clause(ast.ClauseWhere, &ast.ExistsExpression{InnerQuery: itemsQuery(clause(ast.ClauseWhere, column("", "nmae", 0)))}))
				raw.HasReturning = true
				scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, rules)
				_ = analyser.Analyse(t.Context(), &DiagnosticContext{
					Query:       &sqlmodel.AnalysedQuery{Name: "Items", Command: sqlmodel.QueryCommandAsyncExec, Line: 3, SQL: referenceSQL},
					RawAnalysis: raw,
					Scope:       scope,
					Filename:    "items.sql",
				})
			}

			require.Equal(t, readsWhenMade, reads, "the passes read the dialect's Info once, when the analyser is made")
		})
	}
}
