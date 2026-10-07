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

package dialect

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/sqllex"
)

const (
	// lexicalWhitespace lists the characters the lexical rules read as whitespace, which a
	// line comment prefix or a statement terminator must not contain.
	lexicalWhitespace = " \t\r\n\v\f"

	// commentProbeText follows a line comment prefix when InfoProblems checks that the
	// lexical rules read the prefix as the opening of a line comment.
	commentProbeText = " sgl"
)

// InfoProblems checks the metadata required for analysis and SQL generation.
//
// Missing required metadata is found before it can produce misleading query diagnostics
// or invalid SQL.
//
// Takes info (*Info) which is the dialect's description.
//
// Returns []string which names each missing or unusable field in a fixed order, or nil
// when the Info is complete.
func InfoProblems(info *Info) []string {
	var problems []string
	if info.Name == "" {
		problems = append(problems, "Info.Name must name the dialect")
	}
	if info.Family == "" {
		problems = append(problems, "Info.Family must name the dialect's family")
	}
	problems = append(problems, lexicalProblems(info)...)
	if info.Expressions == 0 {
		problems = append(problems, "Info.Expressions must list the expression kinds the dialect supports")
	}
	if info.Placeholders.Execution.Prefix == "" {
		problems = append(problems, "Info.Placeholders.Execution must give the form the generated code writes each parameter in")
	}
	for index, sigil := range info.Placeholders.Sigils {
		if sigil.Prefix == "" {
			problems = append(problems, fmt.Sprintf("Info.Placeholders.Sigils[%d] must give the text that opens the placeholder", index))
		}
	}
	problems = append(problems, generatedSyntaxProblems(info)...)
	return problems
}

// lexicalProblems checks the line-comment prefix and statement terminator against the
// dialect's lexical rules.
//
// The line-comment prefix and statement terminator must contain no whitespace.
//
// Takes info (*Info) which is the dialect's description.
//
// Returns []string which names each problem found, or nil.
func lexicalProblems(info *Info) []string {
	var problems []string
	prefix := info.Comments.LinePrefix
	switch {
	case prefix == "" || strings.ContainsAny(prefix, lexicalWhitespace):
		problems = append(problems, fmt.Sprintf("Info.Comments.LinePrefix must be a line comment prefix without whitespace, got %q", prefix))
	case !opensLineComment(info.Lexical, prefix):
		problems = append(problems, fmt.Sprintf("Info.Comments.LinePrefix %q must open a line comment under Info.Lexical", prefix))
	}
	if strings.ContainsAny(info.Lexical.Terminator, lexicalWhitespace) {
		problems = append(problems, fmt.Sprintf("Info.Lexical.Terminator must not contain whitespace, got %q", info.Lexical.Terminator))
	}
	return problems
}

// generatedSyntaxProblems checks list, pagination and cast syntax required by generated
// code.
//
// Array membership forms must be paired; casts require separator text and a target-type
// list.
//
// Takes info (*Info) which is the dialect's description.
//
// Returns []string which names each problem found, or nil.
func generatedSyntaxProblems(info *Info) []string {
	var problems []string
	if strings.TrimSpace(info.Lists.EmptyMembership) == "" {
		problems = append(problems, "Info.Lists.EmptyMembership must give the test IN and an empty list are written as")
	}
	if strings.TrimSpace(info.Lists.EmptyExclusion) == "" {
		problems = append(problems, "Info.Lists.EmptyExclusion must give the test NOT IN and an empty list are written as")
	}
	if (info.Lists.ArrayMembership == Affix{}) != (info.Lists.ArrayExclusion == Affix{}) {
		problems = append(problems, "Info.Lists.ArrayMembership and Info.Lists.ArrayExclusion must be given together")
	}
	if info.Pagination.Limit == (Affix{}) {
		problems = append(problems, "Info.Pagination.Limit must give the text written around a row limit")
	}
	for index, cast := range info.Filters.Casts {
		if cast.Middle == "" {
			problems = append(problems, fmt.Sprintf("Info.Filters.Casts[%d] must give the text between the value and the type", index))
		}
	}
	if len(info.Filters.Casts) > 0 && len(info.Filters.CastTypes) == 0 {
		problems = append(problems, "Info.Filters.CastTypes must list the types Info.Filters.Casts convert to")
	}
	return problems
}

// opensLineComment reports whether the lexical rules read a line that starts with prefix
// as one line comment.
//
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes prefix (string) which is the line comment prefix.
//
// Returns bool which is true when the whole line is one comment opened by prefix.
func opensLineComment(rules sqllex.Rules, prefix string) bool {
	line := prefix + commentProbeText
	token, ok := sqllex.NewScanner(rules, line).Next()
	return ok && token.Kind == sqllex.KindComment && token.End == len(line)
}
