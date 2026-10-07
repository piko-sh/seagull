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
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ValidateDuplicateNames checks for duplicate query names across all analysed queries,
// which only a view across every query file can see; per-query diagnostics are the
// passes' of DiagnosticAnalyser.
//
// Names are compared regardless of letter case and of the underscores between words, as a
// snake_case name becomes the same method as its PascalCase form: get_user and GetUser
// are duplicates.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which holds the queries to check for
// duplicate names.
//
// Returns []diag.SourceError which holds a Q006 diagnostic for each duplicate found.
func ValidateDuplicateNames(queries []*sqlmodel.AnalysedQuery) []diag.SourceError {
	var diagnostics []diag.SourceError
	seen := make(map[string]*sqlmodel.AnalysedQuery)

	for _, query := range queries {
		key := strings.ToLower(strings.ReplaceAll(query.Name, "_", ""))
		if existing, exists := seen[key]; exists {
			diagnostics = append(diagnostics, diagbuild.Block(query.Filename, query.Line, diag.CodeDuplicateQueryName,
				diag.SeverityError, fmt.Sprintf(
					"duplicate query name %q (first defined in %s:%d)",
					query.Name, existing.Filename, existing.Line,
				)))
		} else {
			seen[key] = query
		}
	}

	return diagnostics
}
