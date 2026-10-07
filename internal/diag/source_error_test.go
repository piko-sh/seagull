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

package diag_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

func TestErrorSeverityString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expected string
		severity diag.ErrorSeverity
	}{
		{severity: diag.SeverityError, expected: "error"},
		{severity: diag.SeverityWarning, expected: "warning"},
		{severity: diag.SeverityHint, expected: "hint"},
		{severity: diag.ErrorSeverity(9), expected: "ErrorSeverity(9)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.expected, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.severity.String())
		})
	}
}

func TestErrorSeverityTravelsInJSONAsItsName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		encoded  string
		severity diag.ErrorSeverity
	}{
		{name: "error", severity: diag.SeverityError, encoded: `{"Severity":"error"}`},
		{name: "warning", severity: diag.SeverityWarning, encoded: `{"Severity":"warning"}`},
		{name: "hint", severity: diag.SeverityHint, encoded: `{"Severity":"hint"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(struct{ Severity diag.ErrorSeverity }{Severity: testCase.severity})
			require.NoError(t, err)
			require.JSONEq(t, testCase.encoded, string(encoded))

			var decoded struct{ Severity diag.ErrorSeverity }
			decoded.Severity = diag.ErrorSeverity(7)
			require.NoError(t, json.Unmarshal([]byte(testCase.encoded), &decoded))
			require.Equal(t, testCase.severity, decoded.Severity)
		})
	}
}

func TestADiagnosticEncodesItsSeverityByName(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(diag.SourceError{Filename: "q.sql", Message: "m", Code: "Q001", Suggestion: "", Line: 1, Column: 2,
		EndLine: 1, EndColumn: 3, Severity: diag.SeverityWarning})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"Severity":"warning"`)
}

func TestErrorSeverityRefusesWhatItCannotName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
	}{
		{name: "an unknown name", text: "fatal"},
		{name: "another case", text: "Warning"},
		{name: "empty", text: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var severity diag.ErrorSeverity
			require.Error(t, severity.UnmarshalText([]byte(testCase.text)))
		})
	}

	_, err := diag.ErrorSeverity(9).MarshalText()
	require.Error(t, err)
}
