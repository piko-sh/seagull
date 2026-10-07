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
	"fmt"
	"strconv"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parameterCountPass checks that all declared parameters are referenced in the SQL query
// body. Unreferenced parameters (except sgl.sortable which is used for dynamic ORDER BY)
// produce a Q042 error.
type parameterCountPass struct {
	// dialect holds what the pass reads of the dialect, its placeholder forms; nil when
	// there is no dialect.
	dialect *dialectFacts
}

// Analyse checks for unreferenced parameters.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds any Q042 diagnostics for unreferenced
// parameters.
func (p *parameterCountPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if subject.RawAnalysis == nil {
		return nil
	}

	referencedNumbers, referencedNames := collectReferencedParameters(subject.RawAnalysis.ParameterReferences)

	var diagnostics []diag.SourceError
	for _, parameterDirective := range subject.ParameterDirectives {
		if parameterDirective.Kind == sqlmodel.ParameterDirectiveSortable {
			continue
		}
		if parameterIsReferenced(parameterDirective, referencedNames, referencedNumbers) {
			continue
		}
		diagnostics = append(diagnostics, unreferencedParameterDiagnostic(parameterDirective, subject, p.dialect))
	}

	return diagnostics
}

// collectReferencedParameters builds the sets of parameter numbers and names that the
// query actually references.
//
// Takes references ([]ast.RawParameterReference) which are the query's uses.
//
// Returns the set of referenced numbers and the set of referenced names.
func collectReferencedParameters(references []ast.RawParameterReference) (map[int]struct{}, map[string]struct{}) {
	referencedNumbers := make(map[int]struct{}, len(references))
	referencedNames := make(map[string]struct{}, len(references))
	for _, reference := range references {
		referencedNumbers[reference.Number] = struct{}{}
		if reference.Name != "" {
			referencedNames[reference.Name] = struct{}{}
		}
	}
	return referencedNumbers, referencedNames
}

// parameterIsReferenced checks whether a query uses a declared parameter.
//
// Named anchors match by their bound identifier, including renamed parameters. Positional
// anchors match by number.
//
// Takes parameterDirective (*directive.ParameterDirective) which is the declared
// parameter.
// Takes referencedNames (map[string]struct{}) which contains parameter names referenced
// by the query.
// Takes referencedNumbers (map[int]struct{}) which contains positional parameter numbers
// referenced by the query.
//
// Returns bool which is true when the parameter is referenced.
func parameterIsReferenced(parameterDirective *directive.ParameterDirective, referencedNames map[string]struct{}, referencedNumbers map[int]struct{}) bool {
	if parameterDirective.IsNamed {
		anchor := parameterDirective.Anchor
		if anchor == "" {
			anchor = parameterDirective.Name
		}
		_, named := referencedNames[anchor]
		return named
	}
	_, numbered := referencedNumbers[parameterDirective.Number]
	return numbered
}

// unreferencedParameterDiagnostic builds the Q-coded error for a
// declared-but-unreferenced parameter, at its directive where the directive was located,
// with how the dialect writes a numbered placeholder when a named anchor spells a number.
//
// Takes parameterDirective (*directive.ParameterDirective) which is the unreferenced
// parameter.
// Takes subject (*DiagnosticContext) which provides the source location.
// Takes facts (*dialectFacts) which gives the dialect's placeholder forms, or nil.
//
// Returns diag.SourceError which is the error diagnostic.
func unreferencedParameterDiagnostic(
	parameterDirective *directive.ParameterDirective,
	subject *DiagnosticContext,
	facts *dialectFacts,
) diag.SourceError {
	message := fmt.Sprintf("parameter %q declared but not referenced in query", parameterDirective.Name)
	if !parameterDirective.IsNamed {
		message = fmt.Sprintf("parameter %d declared as %q but not referenced in query", parameterDirective.Number, parameterDirective.Name)
	} else if numbered := numberedSpelling(parameterDirective.Anchor, facts); numbered != "" {
		message += fmt.Sprintf("; a placeholder numbered %s is written %s", parameterDirective.Anchor, numbered)
	}
	location := diagbuild.TextSpan{Line: subject.Query.Line, Column: 1, EndLine: 0, EndColumn: 0}
	if parameterDirective.Span.Line > 0 {
		location = parameterDirective.Span
	}
	return diagbuild.Located(subject.Filename, location, diag.CodeUnreferencedParameter, diag.SeverityError, message, "")
}

// numberedSpelling returns how the dialect writes a numbered placeholder of the number a
// named anchor spells, as SQLite reads $1 as a placeholder named 1 rather than the first
// placeholder, which it writes ?1.
//
// Takes anchor (string) which is the anchor's name.
// Takes facts (*dialectFacts) which gives the dialect's placeholder forms; nil writes
// none.
//
// Returns string which is the numbered placeholder, or empty when the anchor spells no
// number or the dialect has no numbered form.
func numberedSpelling(anchor string, facts *dialectFacts) string {
	number, err := strconv.Atoi(anchor)
	if err != nil || number <= 0 || facts == nil {
		return ""
	}
	for _, sigil := range facts.info.Placeholders.Sigils {
		if sigil.Kind == dialect.PlaceholderNumbered {
			return sigil.Render(number, "", "")
		}
	}
	return ""
}
