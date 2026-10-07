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

package fault

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/diag"
)

func TestSentinelsWrapTheirParents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		parent  error
		message string
	}{
		{name: "invalid prefix", err: ErrInvalidPrefix, parent: ErrInvalidConfig, message: "invalid prefix"},
		{name: "no source", err: ErrNoSource, parent: ErrInvalidConfig, message: "no source to generate from"},
		{name: "file too large", err: ErrFileTooLarge, parent: ErrLimit, message: "file exceeds the configured size limit"},
		{name: "malformed SQL", err: ErrMalformedSQL, parent: ErrParse, message: "malformed SQL statement"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.message, testCase.err.Error())
			assert.ErrorIs(t, testCase.err, testCase.parent)
			assert.ErrorIs(t, fmt.Errorf("context: %w", testCase.err), testCase.parent)
			assert.NotErrorIs(t, testCase.parent, testCase.err)
		})
	}
}

func TestBaseSentinelsAreDistinct(t *testing.T) {
	t.Parallel()

	bases := []error{ErrInvalidConfig, ErrLimit, ErrParse, ErrPanic, ErrDiagnostics}
	for firstIndex, first := range bases {
		for secondIndex, second := range bases {
			if firstIndex != secondIndex {
				assert.NotErrorIs(t, first, second)
			}
		}
	}
}

func TestHasErrors(t *testing.T) {
	t.Parallel()

	warning := diag.SourceError{Message: "unused", Severity: diag.SeverityWarning}
	failure := diag.SourceError{Message: "unknown table", Severity: diag.SeverityError}

	tests := []struct {
		name        string
		diagnostics []diag.SourceError
		want        bool
	}{
		{name: "no diagnostics", diagnostics: nil, want: false},
		{name: "warnings only", diagnostics: []diag.SourceError{warning}, want: false},
		{name: "one error", diagnostics: []diag.SourceError{warning, failure}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, HasErrors(testCase.diagnostics))
		})
	}
}
