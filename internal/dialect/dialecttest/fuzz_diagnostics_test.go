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

	"seagull.sh/seagull/internal/diag"
)

func TestDiagnosticOracleRejectsImpossibleSourceLocations(t *testing.T) {
	sources := map[string]string{"queries/q.sql": "é\nSELECT 1;"}
	valid := diag.SourceError{Filename: "q.sql", Line: 1, Column: 1, EndLine: 1, EndColumn: 3}
	require.Empty(t, diagnosticLocationProblems([]diag.SourceError{valid}, sources))
	eof := valid
	eof.Line, eof.Column, eof.EndLine, eof.EndColumn = 2, 10, 0, 0
	require.Empty(t, diagnosticLocationProblems([]diag.SourceError{eof}, sources))
	synthetic := diag.SourceError{Line: 1}
	require.Empty(t, diagnosticLocationProblems([]diag.SourceError{synthetic}, sources))
	for _, broken := range []diag.SourceError{
		{Filename: "missing.sql", Line: 1},
		{Filename: "q.sql", Line: 0},
		{Filename: "q.sql", Line: 3, Column: 1},
		{Filename: "q.sql", Line: 1, Column: 4},
		{Filename: "q.sql", Line: 1, Column: 2, EndLine: 1, EndColumn: 1},
		{Filename: "q.sql", Line: 2, Column: 1, EndLine: 3, EndColumn: 1},
	} {
		require.NotEmpty(t, diagnosticLocationProblems([]diag.SourceError{broken}, sources), "%+v", broken)
	}
}
