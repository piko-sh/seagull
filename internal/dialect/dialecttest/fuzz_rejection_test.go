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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestRejectionOracleRejectsMissingWeakenedAndRecoveredErrors(t *testing.T) {
	valid := &app.DetailedResult{Report: &app.Report{}, Result: &app.Result{
		Diagnostics: []diag.SourceError{{Code: diag.CodeCompoundColumnCount, Severity: diag.SeverityError}},
	}}
	require.Empty(t, rejectionResultProblems(valid, diag.CodeCompoundColumnCount))
	for _, diagnostics := range [][]diag.SourceError{
		nil,
		{{Code: diag.CodeCompoundColumnCount, Severity: diag.SeverityWarning}},
		{{Code: diag.CodeMultipleStatements, Severity: diag.SeverityError}},
		{{Code: diag.CodeCompoundColumnCount, Severity: diag.SeverityError}, {Code: diag.CodeRecoveredPanic, Severity: diag.SeverityError}},
	} {
		broken := &app.DetailedResult{Report: &app.Report{}, Result: &app.Result{Diagnostics: diagnostics}}
		require.NotEmpty(t, rejectionResultProblems(broken, diag.CodeCompoundColumnCount))
	}
	require.NotEmpty(t, rejectionResultProblems(nil, diag.CodeCompoundColumnCount))
}

func TestCountDiagnosticOracleRejectsLostSpuriousAndEscalatedHints(t *testing.T) {
	grouped, ok := fuzzcase.Decode("postgres", []byte{1, 3})
	require.True(t, ok)
	plain, ok := fuzzcase.Decode("postgres", []byte{1, 0})
	require.True(t, ok)
	hint := []diag.SourceError{{Code: diag.CodeCountSemanticsWrapped, Severity: diag.SeverityHint}}
	require.Empty(t, countDiagnosticProblems(grouped, hint))
	require.Empty(t, countDiagnosticProblems(plain, nil))
	require.NotEmpty(t, countDiagnosticProblems(grouped, nil))
	require.NotEmpty(t, countDiagnosticProblems(plain, hint))
	for _, severity := range []diag.ErrorSeverity{diag.SeverityWarning, diag.SeverityError} {
		require.NotEmpty(t, countDiagnosticProblems(grouped, []diag.SourceError{{Code: diag.CodeCountSemanticsWrapped, Severity: severity}}))
	}
	require.NotEmpty(t, countDiagnosticProblems(plain, []diag.SourceError{{Code: diag.CodeCountRewriteUnavailable, Severity: diag.SeverityWarning}}))
}
