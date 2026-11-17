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
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// bindingSeedMigration is the schema every binding seed reads.
	bindingSeedMigration = "CREATE TABLE items (d BIGINT);"

	// bindingSeedHeader is the query header every binding seed starts with.
	bindingSeedHeader = "-- sgl.query(Bindings, many)\n"

	// bindingSeedAlternative joins the compared placeholders of a seed.
	bindingSeedAlternative = " OR d = "

	// bindingSeedType is the type annotation a typed placeholder carries.
	bindingSeedType = "Int64"
)

// addBindingSeeds exercises parameter identities and lexical boundaries.
//
// Takes f (*testing.F) which receives the raw SQL seeds.
// Takes info (*dialect.Info) which lists the supported placeholder forms.
func addBindingSeeds(f *testing.F, info *dialect.Info) {
	f.Helper()
	if info.Lexical.DollarInIdentifiers {
		f.Add("CREATE TABLE prices (price$usd BIGINT);", bindingSeedHeader+"SELECT price$usd FROM prices;")
		f.Add("CREATE TABLE prices (price$$usd BIGINT);", bindingSeedHeader+"SELECT price$$usd FROM prices;")
	}
	switch info.Name {
	case "postgres", "timescaledb", "duckdb":
		f.Add(bindingSeedMigration, bindingSeedHeader+"SELECT (ARRAY[1,2,3])[lo$:hi$] FROM (SELECT 1 AS lo$, 2 AS hi$) bounds;")
	}
	addUserVariableSeeds(f, info.Name)
	if info.Lexical.Parameters.QuestionMark && info.Lexical.Parameters.NumberedQuestionMark {
		f.Add(bindingSeedMigration, bindingSeedHeader+"SELECT d FROM items WHERE d = ?1 OR d = ? OR d = ?1;")
		f.Add(bindingSeedMigration, bindingSeedHeader+"SELECT d FROM items WHERE d = ? OR d = ?1 OR d = ?;")
	}
	for _, named := range info.Placeholders.Sigils {
		if named.Kind == dialect.PlaceholderNamed {
			addNamedBindingSeeds(f, info, named)
		}
	}
}

// addNamedBindingSeeds mixes one named form with renamed, dollar and positional forms.
//
// Takes f (*testing.F) which receives the raw SQL seeds.
// Takes info (*dialect.Info) which lists the supported placeholder forms.
// Takes named (dialect.PlaceholderSigil) which is the named form.
func addNamedBindingSeeds(f *testing.F, info *dialect.Info, named dialect.PlaceholderSigil) {
	f.Helper()
	first, second := named.Render(1, "a", bindingSeedType), named.Render(2, "d", bindingSeedType)
	if named.Prefix != "$" || !info.Lexical.Quotes.DollarQuoting {
		dollarFirst, dollarSecond := named.Render(1, "a$one", bindingSeedType), named.Render(2, "b$two", bindingSeedType)
		f.Add(bindingSeedMigration, bindingSeedHeader+"SELECT d FROM items WHERE d = "+dollarFirst+
			bindingSeedAlternative+dollarSecond+bindingSeedAlternative+dollarFirst+";")
	}
	f.Add(bindingSeedMigration, bindingSeedHeader+"-- "+first+" as sgl.param(d)\nSELECT d FROM items WHERE d = "+first+
		bindingSeedAlternative+second+bindingSeedAlternative+named.Render(0, "d_2", bindingSeedType)+";")
	for _, positional := range info.Placeholders.Sigils {
		if positional.Kind != dialect.PlaceholderNamed {
			f.Add(bindingSeedMigration, bindingSeedHeader+"SELECT d FROM items WHERE d = "+positional.Render(1, "", "")+
				bindingSeedAlternative+second+bindingSeedAlternative+second+";")
		}
	}
}

// addUserVariableSeeds covers variable names accepted by MySQL and MariaDB.
//
// Takes f (*testing.F) which receives the raw SQL seeds.
// Takes engine (string) which names the dialect under test.
func addUserVariableSeeds(f *testing.F, engine string) {
	f.Helper()
	if engine != "mysql" && engine != "mariadb" {
		return
	}
	for _, variable := range []string{"@price$usd", "@price.usd", "@'price usd'", "@\"price usd\"", "@`price usd`"} {
		f.Add(bindingSeedMigration, "-- sgl.query(Variable, one)\nSELECT CAST("+variable+" AS CHAR) AS value;")
	}
}

// templateBindingProblems checks parameter identities at their SQL token positions.
//
// Takes info (*dialect.Info) which describes the placeholder forms.
// Takes query (*sqlmodel.AnalysedQuery) which owns the template.
// Takes rendered (emitkit.Rendered) which holds SQL and its driver bindings.
// Takes source ([]sqllex.Token) which holds the original significant tokens.
// Takes output ([]sqllex.Token) which holds the rendered significant tokens.
//
// Returns []string which describes missing or changed bindings.
func templateBindingProblems(info *dialect.Info, query *sqlmodel.AnalysedQuery, rendered emitkit.Rendered, source, output []sqllex.Token) []string {
	want, err := sourceBindingTokens(info, query.Parameters, source)
	if err != nil {
		return []string{fmt.Sprintf("query %s: %v", query.Name, err)}
	}
	got, err := renderedBindingTokens(info, rendered, output)
	if err != nil {
		return []string{fmt.Sprintf("query %s: %v", query.Name, err)}
	}
	if !slices.Equal(want, got) {
		return []string{fmt.Sprintf("query %s changed parameter identities or positions: want %q, got %q", query.Name, want, got)}
	}
	return nil
}

// sourceBindingTokens replaces original placeholders with parameter identities.
//
// Takes info (*dialect.Info) which describes accepted forms.
// Takes parameters ([]sqlmodel.QueryParameter) which holds declared parameters.
// Takes tokens ([]sqllex.Token) which holds source tokens.
//
// Returns []string which retains every token's position.
// Returns error when a placeholder has no corresponding parameter.
func sourceBindingTokens(info *dialect.Info, parameters []sqlmodel.QueryParameter, tokens []sqllex.Token) ([]string, error) {
	result := make([]string, 0, len(tokens))
	highest := 0
	for _, token := range tokens {
		if token.Kind != sqllex.KindParameter {
			result = append(result, token.Raw)
			continue
		}
		reading, ok := info.Placeholders.Read(token.Raw)
		if !ok {
			return nil, fmt.Errorf("unreadable source placeholder %q", token.Raw)
		}
		if reading.Sigil.Kind == dialect.PlaceholderAnonymous || reading.Sigil.Kind == dialect.PlaceholderNumbered && reading.Number == 0 {
			reading.Number = highest + 1
		}
		index, found := sourceParameterIndex(parameters, reading)
		if !found {
			return nil, fmt.Errorf("source placeholder %q has no parameter", token.Raw)
		}
		highest = max(highest, parameters[index].Number)
		result = append(result, bindingIdentity(index, reading.Type, info.Placeholders.Execution.TypeSeparator != ""))
	}
	return result, nil
}

// renderedBindingTokens resolves executed placeholders through driver arguments.
//
// Takes info (*dialect.Info) which describes the execution form.
// Takes rendered (emitkit.Rendered) which holds driver arguments.
// Takes tokens ([]sqllex.Token) which holds executed tokens.
//
// Returns []string which retains every token's position.
// Returns error when an executed placeholder has no driver argument.
func renderedBindingTokens(info *dialect.Info, rendered emitkit.Rendered, tokens []sqllex.Token) ([]string, error) {
	result := make([]string, 0, len(tokens))
	ordinal := 0
	for _, token := range tokens {
		if token.Kind != sqllex.KindParameter {
			result = append(result, token.Raw)
			continue
		}
		reading, ok := info.Placeholders.Read(token.Raw)
		if !ok {
			return nil, fmt.Errorf("unreadable executed placeholder %q", token.Raw)
		}
		index := reading.Number - 1
		switch info.Placeholders.Execution.Kind {
		case dialect.PlaceholderAnonymous:
			index = ordinal
		case dialect.PlaceholderNamed:
			index = slices.IndexFunc(rendered.Arguments, func(argument emitkit.Argument) bool { return argument.Name == reading.Name })
		case dialect.PlaceholderNumbered:
		}
		ordinal++
		if index < 0 || index >= len(rendered.Arguments) {
			return nil, fmt.Errorf("executed placeholder %q has no driver argument", token.Raw)
		}
		result = append(result, bindingIdentity(rendered.Arguments[index].Parameter, reading.Type, info.Placeholders.Execution.TypeSeparator != ""))
	}
	return result, nil
}

// bindingIdentity encodes a parameter index and its significant type annotation.
//
// Takes index (int) which identifies the logical parameter.
// Takes annotation (string) which is the placeholder's type.
// Takes typed (bool) which reports whether execution preserves types.
//
// Returns string which cannot be confused with ordinary SQL text.
func bindingIdentity(index int, annotation string, typed bool) string {
	if !typed {
		annotation = ""
	}
	return bindingIdentityPrefix(index) + annotation + ")"
}

// bindingIdentityPrefix starts the identity of one logical parameter.
//
// Takes index (int) which identifies the logical parameter.
//
// Returns string which every identity of that parameter starts with.
func bindingIdentityPrefix(index int) string {
	return "\x00parameter[" + strconv.Itoa(index) + "]("
}

// sourceParameterIndex finds an original placeholder's logical parameter.
//
// A named placeholder matches its declared placeholder name first, then a parameter
// without one by its own name.
//
// Takes parameters ([]sqlmodel.QueryParameter) which holds declared parameters.
// Takes reading (dialect.PlaceholderReading) which identifies the placeholder.
//
// Returns int which is the parameter index.
// Returns bool which is false when no parameter matches.
func sourceParameterIndex(parameters []sqlmodel.QueryParameter, reading dialect.PlaceholderReading) (int, bool) {
	matches := []func(sqlmodel.QueryParameter) bool{
		func(parameter sqlmodel.QueryParameter) bool { return parameter.Number == reading.Number },
	}
	if reading.Sigil.Kind == dialect.PlaceholderNamed {
		matches = []func(sqlmodel.QueryParameter) bool{
			func(parameter sqlmodel.QueryParameter) bool { return parameter.Placeholder == reading.Name },
			func(parameter sqlmodel.QueryParameter) bool {
				return cmp.Or(parameter.Placeholder, parameter.Name) == reading.Name
			},
		}
	}
	for _, match := range matches {
		if index := slices.IndexFunc(parameters, match); index >= 0 {
			return index, true
		}
	}
	return 0, false
}
