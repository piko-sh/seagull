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
	"fmt"
	"path"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
)

// diagnosticLocationProblems validates located diagnostics against input bytes.
//
// Takes diagnostics ([]diag.SourceError) which came from generation.
// Takes sources (map[string]string) which maps input paths to their contents.
//
// Returns []string which describes unknown files and invalid byte positions.
func diagnosticLocationProblems(diagnostics []diag.SourceError, sources map[string]string) []string {
	lines := make(map[string][]string, len(sources))
	for name, source := range sources {
		lines[name] = strings.Split(source, "\n")
	}
	var problems []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Line < 1 || diagnostic.Column < 0 || diagnostic.EndLine < 0 || diagnostic.EndColumn < 0 {
			problems = append(problems, fmt.Sprintf("%s has invalid diagnostic coordinates: %+v", diagnostic.Code, diagnostic))
			continue
		}
		if diagnostic.Filename == "" {
			continue
		}
		candidates := diagnosticSourceCandidates(diagnostic.Filename, lines)
		if len(candidates) == 0 {
			problems = append(problems, fmt.Sprintf("%s names an unknown source file %q", diagnostic.Code, diagnostic.Filename))
			continue
		}
		fits := func(candidate []string) bool { return diagnosticRangeFits(diagnostic, candidate) }
		if !slices.ContainsFunc(candidates, fits) {
			problems = append(problems, fmt.Sprintf("%s has a diagnostic range outside %s: %+v", diagnostic.Code, diagnostic.Filename, diagnostic))
		}
	}
	return problems
}

// diagnosticPositionFits accepts byte columns and the position immediately after EOF.
//
// Takes lines ([]string) which retains every physical source line.
// Takes line (int) which is a one-based line number.
// Takes column (int) which is a one-based byte column or zero when unavailable.
//
// Returns bool which reports whether the source contains the position.
func diagnosticPositionFits(lines []string, line, column int) bool {
	return line >= 1 && line <= len(lines) && column >= 0 && column <= len(lines[line-1])+1
}

// diagnosticSourceCandidates resolves the filenames exposed by the pipeline.
//
// Takes filename (string) which may be relative to a query or migration directory.
// Takes sources (map[string][]string) which holds the submitted paths and their lines.
//
// Returns [][]string which retains both possibilities when relative names collide.
func diagnosticSourceCandidates(filename string, sources map[string][]string) [][]string {
	if source, ok := sources[filename]; ok {
		return [][]string{source}
	}
	var result [][]string
	for name, source := range sources {
		if path.Base(name) == filename {
			result = append(result, source)
		}
	}
	return result
}

// diagnosticRangeFits validates an optional half-open diagnostic range.
//
// Takes diagnostic (diag.SourceError) which locates an error or warning.
// Takes lines ([]string) which holds the lines of a candidate source file.
//
// Returns bool which reports ordered source positions including EOF.
func diagnosticRangeFits(diagnostic diag.SourceError, lines []string) bool {
	if !diagnosticPositionFits(lines, diagnostic.Line, diagnostic.Column) {
		return false
	}
	if diagnostic.EndLine == 0 && diagnostic.EndColumn == 0 {
		return true
	}
	return diagnosticPositionFits(lines, diagnostic.EndLine, diagnostic.EndColumn) &&
		(diagnostic.EndLine > diagnostic.Line || diagnostic.EndLine == diagnostic.Line && diagnostic.EndColumn >= diagnostic.Column)
}
