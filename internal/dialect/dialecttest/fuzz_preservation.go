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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// templatePreservationProblems checks bytes outside explicit SQL rewrites.
//
// List rendering rewrites the interior of a single slice placeholder's IN list. Bytes
// outside that independently identified span must remain unchanged.
//
// Takes info (*dialect.Info) which supplies lexical and binding rules.
// Takes query (*sqlmodel.AnalysedQuery) which supplies the original SQL.
// Takes rendered (emitkit.Rendered) which preserves stars and ordinary SQL.
//
// Returns []string which describes changed literals, comments or whitespace.
func templatePreservationProblems(info *dialect.Info, query *sqlmodel.AnalysedQuery, rendered emitkit.Rendered) []string {
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	source := syntax.StripDirectives(query.SQL, lexicon, namespace.Default())
	original, originalErr := sqllex.Tokens(info.Lexical, source)
	output, outputErr := sqllex.Tokens(info.Lexical, rendered.SQL)
	if originalErr != nil || outputErr != nil {
		return []string{fmt.Sprintf("preservation tokenisation failed: source %v, output %v", originalErr, outputErr)}
	}
	want, wantErr := sourceBindingTokens(info, query.Parameters, original)
	got, gotErr := renderedBindingTokens(info, rendered, output)
	if wantErr != nil || gotErr != nil {
		return []string{fmt.Sprintf("preservation binding failed: source %v, output %v", wantErr, gotErr)}
	}
	want = preservedListTokens(original, want, query.Parameters)
	got = preservedListTokens(output, got, query.Parameters)
	if !slices.Equal(want, got) {
		return []string{fmt.Sprintf("query %s changed preserved SQL bytes: source %q, output %q", query.Name, source, rendered.SQL)}
	}
	return nil
}

// preservedListTokens exempts only a rewritten slice list's interior spacing.
//
// Takes tokens ([]sqllex.Token) which includes whitespace and comment tokens.
// Takes identities ([]string) which replaces placeholders with logical identities.
// Takes parameters ([]sqlmodel.QueryParameter) which identifies declared slices.
//
// Returns []string which preserves every byte outside an expanded list interior.
func preservedListTokens(tokens []sqllex.Token, identities []string, parameters []sqlmodel.QueryParameter) []string {
	var result []string
	previous := ""
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Raw == "(" && strings.EqualFold(previous, "IN") {
			parameter := nextPreservedToken(tokens, index+1)
			closing := nextPreservedToken(tokens, parameter+1)
			if closing < len(tokens) && tokens[parameter].Kind == sqllex.KindParameter && tokens[closing].Raw == ")" &&
				preservedSliceIdentity(identities[parameter], parameters) {
				result = append(result, "(", identities[parameter], ")")
				index = closing
				previous = ")"
				continue
			}
		}
		result = append(result, identities[index])
		if !token.Kind.IsNoise() {
			previous = token.Raw
		}
	}
	return result
}

// nextPreservedToken locates the next token outside whitespace and comments.
//
// Takes tokens ([]sqllex.Token) which holds a complete token stream.
// Takes start (int) which is the first candidate index.
//
// Returns int which is the next significant index or the stream length.
func nextPreservedToken(tokens []sqllex.Token, start int) int {
	for start < len(tokens) && tokens[start].Kind.IsNoise() {
		start++
	}
	return min(start, len(tokens))
}

// preservedSliceIdentity recognises an independently mapped slice binding.
//
// Takes identity (string) which came from the parameter identity oracle.
// Takes parameters ([]sqlmodel.QueryParameter) which contains declared slice flags.
//
// Returns bool which reports a slice that the rendering mode expands.
func preservedSliceIdentity(identity string, parameters []sqlmodel.QueryParameter) bool {
	for index := range parameters {
		if parameters[index].IsSlice && strings.HasPrefix(identity, bindingIdentityPrefix(index)) {
			return true
		}
	}
	return false
}
