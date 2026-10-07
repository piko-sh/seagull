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

package facade_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

func TestDiagnosticsErrorSummarisesErrorDiagnostics(t *testing.T) {
	t.Parallel()

	warning := seagull.SourceError{Message: "unused", Severity: seagull.SeverityWarning}
	first := seagull.SourceError{Filename: "users.sql", Line: 3, Column: 7, Code: "Q001", Message: "unknown column", Severity: seagull.SeverityError}
	second := seagull.SourceError{Message: "unknown table", Severity: seagull.SeverityError}

	tests := []struct {
		name        string
		diagnostics []seagull.SourceError
		message     string
	}{
		{name: "no diagnostics", diagnostics: nil, message: ""},
		{name: "warnings only", diagnostics: []seagull.SourceError{warning}, message: ""},
		{name: "one error", diagnostics: []seagull.SourceError{warning, first}, message: "1 error diagnostic: users.sql:3:7: [Q001] unknown column"},
		{name: "two errors", diagnostics: []seagull.SourceError{second, first}, message: "2 error diagnostics: unknown table"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := &seagull.Result{Diagnostics: testCase.diagnostics}
			report := &seagull.Report{Diagnostics: testCase.diagnostics}
			for _, err := range []error{result.Err(), report.Err()} {
				if testCase.message == "" {
					require.NoError(t, err)
					continue
				}
				require.Error(t, err)
				assert.Equal(t, testCase.message, err.Error())
				assert.ErrorIs(t, err, seagull.ErrDiagnostics)
				diagnosticsError, isDiagnosticsError := errors.AsType[*seagull.DiagnosticsError](err)
				require.True(t, isDiagnosticsError)
				assert.Equal(t, testCase.diagnostics, diagnosticsError.Diagnostics)
			}
		})
	}
}

func TestDiagnosticsErrorWithoutErrorsFallsBackToSentinelText(t *testing.T) {
	t.Parallel()

	err := &seagull.DiagnosticsError{Diagnostics: nil}

	assert.Equal(t, seagull.ErrDiagnostics.Error(), err.Error())
}

func TestDiagnosticsErrorMatchesErrParseForUnparsableSQL(t *testing.T) {
	t.Parallel()

	parse := seagull.SourceError{Code: seagull.CodeParseError, Message: "failed to parse", Severity: seagull.SeverityError}
	parseWarning := seagull.SourceError{Code: seagull.CodeParseError, Message: "failed to parse", Severity: seagull.SeverityWarning}
	unknown := seagull.SourceError{Code: seagull.CodeUnknownColumn, Message: "unknown column", Severity: seagull.SeverityError}

	tests := []struct {
		name        string
		diagnostics []seagull.SourceError
		wantParse   bool
	}{
		{name: "a parse error", diagnostics: []seagull.SourceError{unknown, parse}, wantParse: true},
		{name: "no parse error", diagnostics: []seagull.SourceError{unknown}, wantParse: false},
		{name: "a parse warning beside another error", diagnostics: []seagull.SourceError{unknown, parseWarning}, wantParse: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := (&seagull.Report{Diagnostics: testCase.diagnostics}).Err()
			require.ErrorIs(t, err, seagull.ErrDiagnostics)
			assert.Equal(t, testCase.wantParse, errors.Is(err, seagull.ErrParse))
			assert.NotErrorIs(t, err, seagull.ErrMalformedSQL)
		})
	}
}
