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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type otherDetail struct{}

func (otherDetail) EngineDetail() {}

func TestAsyncExecPass_HardErrorOnUnsupportedEngine(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
		DialectFunc:                func() string { return "postgres" },
		SupportsAsyncMutationsFunc: func() bool { return false },
	})}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "PurgeOld",
			Command: sqlmodel.QueryCommandAsyncExec,
			Line:    7,
		},
		RawAnalysis: &ast.RawQueryAnalysis{},
	}

	diagnostics := pass.Analyse(t.Context(), subject)

	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeAsyncExecNotSupported, diagnostics[0].Code)
	assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
	assert.Contains(t, diagnostics[0].Message, `"PurgeOld"`)
	assert.Contains(t, diagnostics[0].Message, `dialect "postgres"`)
	assert.Contains(t, diagnostics[0].Message, "asynchronous mutation semantics")
}

func TestAsyncExecPass_AcceptsAsyncExecOnSupportingEngine(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
		DialectFunc:                func() string { return "clickhouse" },
		SupportsAsyncMutationsFunc: func() bool { return true },
	})}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "PurgeOld",
			Command: sqlmodel.QueryCommandAsyncExec,
			Line:    7,
		},
		RawAnalysis: &ast.RawQueryAnalysis{
			AsyncMutation: true,
		},
	}

	diagnostics := pass.Analyse(t.Context(), subject)
	assert.Empty(t, diagnostics)
}

func TestAsyncExecPass_RecommendsAsyncExecForExecOnSupportingEngine(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
		DialectFunc:                func() string { return "clickhouse" },
		SupportsAsyncMutationsFunc: func() bool { return true },
	})}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "PurgeOld",
			Command: sqlmodel.QueryCommandExec,
			Line:    9,
		},
		RawAnalysis: &ast.RawQueryAnalysis{
			AsyncMutation: true,
		},
	}

	diagnostics := pass.Analyse(t.Context(), subject)

	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodeAsyncExecRecommended, diagnostics[0].Code)
	assert.Equal(t, diag.SeverityHint, diagnostics[0].Severity)
	assert.Contains(t, diagnostics[0].Message, `"PurgeOld"`)
	assert.Contains(t, diagnostics[0].Message, "asyncexec")
}

func TestAsyncExecPass_QuietOnExecWithoutAsyncMarker(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
		DialectFunc:                func() string { return "clickhouse" },
		SupportsAsyncMutationsFunc: func() bool { return true },
	})}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "InsertRow",
			Command: sqlmodel.QueryCommandExec,
			Line:    3,
		},
		RawAnalysis: &ast.RawQueryAnalysis{},
	}

	diagnostics := pass.Analyse(t.Context(), subject)
	assert.Empty(t, diagnostics)
}

func TestAsyncExecPass_QuietOnExecWithAsyncMarkerButUnsupportedEngine(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
		DialectFunc:                func() string { return "postgres" },
		SupportsAsyncMutationsFunc: func() bool { return false },
	})}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "ExecQuery",
			Command: sqlmodel.QueryCommandExec,
			Line:    3,
		},
		RawAnalysis: &ast.RawQueryAnalysis{
			AsyncMutation: true,
		},
	}

	diagnostics := pass.Analyse(t.Context(), subject)
	assert.Empty(t, diagnostics)
}

func TestAsyncExecPass_QuietWhenEngineIsNil(t *testing.T) {
	t.Parallel()

	pass := &asyncExecPass{dialect: newDialectFacts(nil)}
	subject := &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:    "PurgeOld",
			Command: sqlmodel.QueryCommandAsyncExec,
			Line:    7,
		},
		RawAnalysis: &ast.RawQueryAnalysis{},
	}

	diagnostics := pass.Analyse(t.Context(), subject)
	assert.Empty(t, diagnostics)
}

func TestAsyncExecPass_QuietOnNonExecCommands(t *testing.T) {
	t.Parallel()

	commands := []struct {
		name    string
		command sqlmodel.QueryCommand
	}{
		{name: "one", command: sqlmodel.QueryCommandOne},
		{name: "many", command: sqlmodel.QueryCommandMany},
		{name: "execresult", command: sqlmodel.QueryCommandExecResult},
		{name: "execrows", command: sqlmodel.QueryCommandExecRows},
	}

	for _, testCase := range commands {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &asyncExecPass{dialect: newDialectFacts(&fakedialect.Engine{
				DialectFunc:                func() string { return "clickhouse" },
				SupportsAsyncMutationsFunc: func() bool { return true },
			})}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:    "Q",
					Command: testCase.command,
					Line:    1,
				},
				RawAnalysis: &ast.RawQueryAnalysis{
					AsyncMutation: true,
				},
			}

			diagnostics := pass.Analyse(t.Context(), subject)
			assert.Empty(t, diagnostics)
		})
	}
}

func TestIsAsyncMutation(t *testing.T) {
	t.Parallel()

	t.Run("nil analysis returns false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, isAsyncMutation(nil))
	})

	t.Run("an analysis that is no asynchronous mutation returns false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, isAsyncMutation(&ast.RawQueryAnalysis{}))
	})

	t.Run("an asynchronous mutation returns true", func(t *testing.T) {
		t.Parallel()

		assert.True(t, isAsyncMutation(&ast.RawQueryAnalysis{AsyncMutation: true}))
	})

	t.Run("a dialect's own detail does not mark a mutation", func(t *testing.T) {
		t.Parallel()

		assert.False(t, isAsyncMutation(&ast.RawQueryAnalysis{Detail: otherDetail{}}))
	})
}
