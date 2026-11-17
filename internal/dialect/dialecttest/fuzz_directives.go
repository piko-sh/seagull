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
	"maps"
	"reflect"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
)

// fuzzDirectiveBlock retains a parsed header and its diagnostic categories.
type fuzzDirectiveBlock struct {
	// value is the semantic header with source spans cleared.
	value *directive.DirectiveBlock

	// diagnostics retains errors so malformed headers are not compared as valid trees.
	diagnostics []diag.SourceError
}

// directiveMeaningProblems compares valid parsed values before and after formatting.
//
// Takes info (*dialect.Info) which supplies the actual dialect syntax.
// Takes prefixes (namespace.Set) which accepts canonical and alias spellings.
// Takes source (string) which is the original SQL file.
// Takes formatted (string) which is the formatter's output.
//
// Returns []string which describes changed directive values or diagnostic categories.
func directiveMeaningProblems(info *dialect.Info, prefixes namespace.Set, source, formatted string) []string {
	problems := queryDirectiveMeaningProblems(info, prefixes, source, formatted)
	return append(problems, migrationDirectiveMeaningProblems(info, prefixes, source, formatted)...)
}

// queryDirectiveMeaningProblems compares valid query headers before and after formatting.
//
// A file the splitter truncated has no stable block structure, so its headers are not
// compared. The lexical fuzzer still checks its idempotence and source preservation.
//
// Takes info (*dialect.Info) which supplies the actual dialect syntax.
// Takes prefixes (namespace.Set) which accepts canonical and alias spellings.
// Takes source (string) which is the original SQL file.
// Takes formatted (string) which is the formatter's output.
//
// Returns []string which describes lost or changed query headers.
func queryDirectiveMeaningProblems(info *dialect.Info, prefixes namespace.Set, source, formatted string) []string {
	originalBlocks, truncated := parsedFuzzDirectiveBlocks(info, prefixes, source)
	if truncated {
		return nil
	}
	formattedBlocks, _ := parsedFuzzDirectiveBlocks(info, prefixes, formatted)
	var problems []string
	for _, line := range slices.Sorted(maps.Keys(originalBlocks)) {
		before := originalBlocks[line]
		if containsErrorDiagnostic(before.diagnostics) {
			continue
		}
		after, ok := formattedBlocks[line]
		if !ok {
			problems = append(problems, fmt.Sprintf("formatting lost the valid query directive on line %d", line))
			continue
		}
		problems = append(problems, validDirectiveMeaningProblems(before.value, after.value, before.diagnostics, after.diagnostics, "query")...)
	}
	return problems
}

// containsErrorDiagnostic reports whether any diagnostic is an error.
//
// Takes diagnostics ([]diag.SourceError) which came from parsing.
//
// Returns bool which is true when at least one is an error.
func containsErrorDiagnostic(diagnostics []diag.SourceError) bool {
	return slices.ContainsFunc(diagnostics, func(problem diag.SourceError) bool { return problem.Severity == diag.SeverityError })
}

// migrationDirectiveMeaningProblems checks migration values and statement attachment.
//
// Takes info (*dialect.Info) which supplies lexical facts.
// Takes prefixes (namespace.Set) which accepts directive namespaces.
// Takes source (string) which holds the original migration.
// Takes formatted (string) which holds the formatted migration.
//
// Returns []string which describes changed metadata or diagnostic categories.
func migrationDirectiveMeaningProblems(info *dialect.Info, prefixes namespace.Set, source, formatted string) []string {
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	before, beforeErrors := syntax.ParseMigration(source, lexicon, prefixes, fuzzQueryFile)
	after, afterErrors := syntax.ParseMigration(formatted, lexicon, prefixes, fuzzQueryFile)
	if containsErrorDiagnostic(beforeErrors) {
		return nil
	}
	clearDirectiveSpans(reflect.ValueOf(&before))
	clearDirectiveSpans(reflect.ValueOf(&after))
	beforeOrdinals, beforeValid := directiveStatementOrdinals(info.Lexical, source, before.ReadOnlyOverrides)
	afterOrdinals, afterValid := directiveStatementOrdinals(info.Lexical, formatted, after.ReadOnlyOverrides)
	if !beforeValid || !afterValid {
		return []string{"migration directive attachment does not identify a SQL token"}
	}
	before.ReadOnlyOverrides, after.ReadOnlyOverrides = beforeOrdinals, afterOrdinals
	return validDirectiveMeaningProblems(before, after, beforeErrors, afterErrors, "migration")
}

// clearDirectiveSpans clears only typed source positions in a parsed directive tree.
//
// Takes value (reflect.Value) which points into an exclusively owned parsed result.
func clearDirectiveSpans(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Type() == reflect.TypeFor[diagbuild.TextSpan]() && value.CanSet() {
		value.SetZero()
		return
	}
	if value.Kind() == reflect.Pointer {
		clearDirectiveSpans(value.Elem())
	}
	if value.Kind() == reflect.Struct {
		for _, field := range value.Fields() {
			clearDirectiveSpans(field)
		}
	}
	if value.Kind() == reflect.Slice {
		for index := range value.Len() {
			clearDirectiveSpans(value.Index(index))
		}
	}
}

// directiveDiagnosticKinds retains diagnostic categories while discarding shifted
// locations.
//
// Takes diagnostics ([]diag.SourceError) which came from parsing.
//
// Returns []string which records ordered codes and severities.
func directiveDiagnosticKinds(diagnostics []diag.SourceError) []string {
	result := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		result = append(result, fmt.Sprintf("%s/%d", diagnostic.Code, diagnostic.Severity))
	}
	return result
}

// directiveStatementOrdinals maps byte offsets onto stable SQL token positions.
//
// Takes rules (sqllex.Rules) which describes the dialect.
// Takes source (string) which owns the byte offsets.
// Takes offsets (map[int]*bool) which attaches overrides to statement starts.
//
// Returns map[int]*bool which retains the statement identities after formatting.
// Returns bool which confirms that every attachment was retained.
func directiveStatementOrdinals(rules sqllex.Rules, source string, offsets map[int]*bool) (map[int]*bool, bool) {
	result := make(map[int]*bool, len(offsets))
	ordinal := 0
	for _, token := range syntax.Tokens(rules, source) {
		if token.Kind.IsNoise() {
			continue
		}
		if value, ok := offsets[token.Start]; ok {
			result[ordinal] = value
		}
		ordinal++
	}
	return result, len(result) == len(offsets)
}

// formatFuzzSource uses the same accepted anchor forms as the dialect-aware entry point.
//
// Takes info (*dialect.Info) which supplies lexical and directive anchor rules.
// Takes source (string) which contains the original SQL.
// Takes prefixes (namespace.Set) which selects accepted directive namespaces.
//
// Returns string which contains the formatted SQL.
func formatFuzzSource(info *dialect.Info, source string, prefixes namespace.Set) string {
	sigils := make([]syntax.SigilForm, 0, len(info.Placeholders.Sigils))
	for _, sigil := range info.Placeholders.Sigils {
		sigils = append(sigils, syntax.SigilForm{Prefix: sigil.Prefix, Suffix: sigil.Suffix})
	}
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	return syntax.FormatSource(source, lexicon, prefixes, sigils...)
}

// validDirectiveMeaningProblems compares successfully parsed directive trees.
//
// Malformed inputs have no stable semantic tree. Their lexical fuzzer still checks
// idempotence, source preservation and termination after permitted spacing changes.
//
// Takes before (T) which is the original tree without source spans.
// Takes after (T) which is the formatted tree without source spans.
// Takes beforeErrors ([]diag.SourceError) which holds original diagnostics.
// Takes afterErrors ([]diag.SourceError) which holds formatted diagnostics.
// Takes kind (string) which identifies query or migration parsing.
//
// Returns []string which describes a changed valid directive meaning.
func validDirectiveMeaningProblems[T any](before, after T, beforeErrors, afterErrors []diag.SourceError, kind string) []string {
	if containsErrorDiagnostic(beforeErrors) {
		return nil
	}
	var problems []string
	if !reflect.DeepEqual(before, after) {
		problems = append(problems, "formatting changed parsed "+kind+" directive values")
	}
	if !slices.Equal(directiveDiagnosticKinds(beforeErrors), directiveDiagnosticKinds(afterErrors)) {
		problems = append(problems, "formatting changed "+kind+" directive diagnostic categories")
	}
	return problems
}

// parsedFuzzDirectiveBlocks indexes headers by their unchanged physical name lines.
//
// Takes info (*dialect.Info) which supplies actual parsing rules.
// Takes prefixes (namespace.Set) which selects accepted namespaces.
// Takes source (string) which holds a query file.
//
// Returns map[int]fuzzDirectiveBlock which keeps every named header separately.
// Returns bool which is true when the splitter truncated the file.
func parsedFuzzDirectiveBlocks(info *dialect.Info, prefixes namespace.Set, source string) (map[int]fuzzDirectiveBlock, bool) {
	parser := directive.NewParser(info.Placeholders.Sigils, info.Comments, directive.PrefixPolicy{Prefixes: prefixes, RequireCanonical: false})
	querySyntax := sqlfile.QuerySyntax{Prefixes: prefixes, Directives: directive.Roles(),
		Lexicon: syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}}
	blocks, truncatedLine := sqlfile.SplitQueryFile([]byte(source), querySyntax)
	result := make(map[int]fuzzDirectiveBlock)
	for index := range blocks {
		parsed, diagnostics := parser.Parse(blocks[index], fuzzQueryFile)
		if parsed.Name == nil {
			continue
		}
		line := parsed.Name.ValueSpan.Line
		clearDirectiveSpans(reflect.ValueOf(parsed))
		result[line] = fuzzDirectiveBlock{value: parsed, diagnostics: diagnostics}
	}
	return result, truncatedLine > 0
}
