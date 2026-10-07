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

package emittest

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// punctuationOpen opens a parenthesis.
	punctuationOpen = "("

	// punctuationClose closes a parenthesis.
	punctuationClose = ")"

	// punctuationComma separates list items.
	punctuationComma = ","
)

// statementScan is a statement's significant tokens, each with its parenthesis depth.
type statementScan struct {
	// tokens holds the significant tokens.
	tokens []sqllex.Token

	// depths holds each token's parenthesis depth; a parenthesis has the depth outside it.
	depths []int
}

// inListPlaceholders counts the placeholders of each IN list.
//
// Returns []int which holds one count per list, in order.
func (scan statementScan) inListPlaceholders() []int {
	var counts []int
	for index := range len(scan.tokens) - 1 {
		if !scan.tokens[index].IsWord("IN") || scan.tokens[index+1].Raw != punctuationOpen {
			continue
		}
		count := 0
		for position := index + 2; position < scan.closing(index+1); position++ {
			if scan.tokens[position].Kind == sqllex.KindParameter {
				count++
			}
		}
		counts = append(counts, count)
	}
	return counts
}

// clauses returns the positions of a clause keyword outside parentheses: a word, or two
// words in a row such as ORDER BY.
//
// Takes words (...string) which are the keyword's words.
//
// Returns []int which holds the position of each first word.
func (scan statementScan) clauses(words ...string) []int {
	var positions []int
	for index := range scan.tokens {
		if scan.depths[index] != 0 || index+len(words) > len(scan.tokens) {
			continue
		}
		matched := true
		for offset, word := range words {
			matched = matched && scan.tokens[index+offset].IsWord(word)
		}
		if matched {
			positions = append(positions, index)
		}
	}
	return positions
}

// emptyLists counts IN lists with nothing between their parentheses.
//
// Returns int which is the count.
func (scan statementScan) emptyLists() int {
	count := 0
	for index := range len(scan.tokens) - 2 {
		if scan.tokens[index].IsWord("IN") && scan.tokens[index+1].Raw == punctuationOpen && scan.tokens[index+2].Raw == punctuationClose {
			count++
		}
	}
	return count
}

// valuesRows finds the rows of the statement's VALUES clause outside parentheses.
//
// Returns int which is the number of rows.
// Returns int which counts placeholders outside the rows.
func (scan statementScan) valuesRows() (rows, outside int) {
	positions := scan.clauses("VALUES")
	inside := make([]bool, len(scan.tokens))
	if len(positions) > 0 {
		for index := positions[0] + 1; index < len(scan.tokens) && scan.tokens[index].Raw == punctuationOpen; {
			end := scan.closing(index)
			for position := index; position <= end; position++ {
				inside[position] = true
			}
			rows++
			if end+2 >= len(scan.tokens) || scan.tokens[end+1].Raw != punctuationComma {
				break
			}
			index = end + 2
		}
	}
	for index, token := range scan.tokens {
		if token.Kind == sqllex.KindParameter && !inside[index] {
			outside++
		}
	}
	return rows, outside
}

// closing returns the position of the parenthesis that closes the one at a position.
//
// Takes open (int) which is the opening parenthesis's position.
//
// Returns int which is the closing one's position, or the last token's.
func (scan statementScan) closing(open int) int {
	for index := open + 1; index < len(scan.tokens); index++ {
		if scan.depths[index] == scan.depths[open] && scan.tokens[index].Raw == punctuationClose {
			return index
		}
	}
	return len(scan.tokens) - 1
}

// conditionMixesConnectives reports whether the WHERE condition outside parentheses joins
// terms with both OR and AND, so a term appended with AND binds to the last OR branch
// only.
//
// Returns bool which is true when it does.
func (scan statementScan) conditionMixesConnectives() bool {
	where := scan.clauses("WHERE")
	if len(where) == 0 {
		return false
	}
	hasOr, hasAnd := false, false
	for index := where[0] + 1; index < len(scan.tokens); index++ {
		token := scan.tokens[index]
		if scan.depths[index] != 0 {
			continue
		}
		if slices.ContainsFunc([]string{"GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "FETCH"}, token.IsWord) {
			break
		}
		hasOr = hasOr || token.IsWord("OR")
		hasAnd = hasAnd || token.IsWord("AND")
	}
	return hasOr && hasAnd
}

// placeholderCount is what a statement's placeholders say about the arguments it binds.
type placeholderCount struct {
	// numbers holds the number of each numbered placeholder.
	numbers map[int]bool

	// names holds the name of each named placeholder.
	names map[string]bool

	// foreign lists placeholders in neither the execution nor the positional form.
	foreign []string

	// forms lists the forms the placeholders take.
	forms []dialect.PlaceholderSigil

	// anonymous counts anonymous placeholders, and numbered ones written without a number.
	anonymous int
}

// scanStatement lexes a statement and records each significant token's depth.
//
// Takes text (string) which is the statement.
// Takes info (*dialect.Info) which supplies the lexical rules.
//
// Returns statementScan which holds the tokens.
// Returns error when the statement does not lex.
func scanStatement(text string, info *dialect.Info) (statementScan, error) {
	tokens, err := sqllex.Tokens(executionRules(info), text)
	if err != nil {
		return statementScan{tokens: nil, depths: nil}, err
	}
	scan := statementScan{tokens: nil, depths: nil}
	depth := 0
	for _, token := range tokens {
		if token.Kind.IsNoise() {
			continue
		}
		if token.Kind == sqllex.KindPunctuation && token.Raw == punctuationClose {
			depth--
		}
		scan.tokens = append(scan.tokens, token)
		scan.depths = append(scan.depths, depth)
		if token.Kind == sqllex.KindPunctuation && token.Raw == punctuationOpen {
			depth++
		}
	}
	return scan, nil
}

// executionRules returns the dialect's lexical rules, extended where they do not read a
// placeholder of the execution or positional form as one parameter token, so the
// placeholders the generated code writes are counted whatever the rules cover.
//
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns sqllex.Rules which reads both forms.
func executionRules(info *dialect.Info) sqllex.Rules {
	rules := info.Lexical
	for _, sigil := range []dialect.PlaceholderSigil{info.Placeholders.Execution, info.Placeholders.PositionalForm()} {
		sample := sigil.Render(1, "name", "Type")
		if readsAsParameter(rules, sample) {
			continue
		}
		for _, extended := range parameterRuleExtensions(rules, sigil) {
			if readsAsParameter(extended, sample) {
				rules = extended
				break
			}
		}
	}
	return rules
}

// parameterRuleExtensions returns the rules extended in each way sqllex can read one more
// placeholder form.
//
// Takes rules (sqllex.Rules) which are the rules to extend.
// Takes sigil (dialect.PlaceholderSigil) which is the form to read.
//
// Returns []sqllex.Rules which holds one extension per way.
func parameterRuleExtensions(rules sqllex.Rules, sigil dialect.PlaceholderSigil) []sqllex.Rules {
	dollar, question, numberedQuestion, braced, named := rules, rules, rules, rules, rules
	dollar.Parameters.DollarNumbers = true
	question.Parameters.QuestionMark = true
	numberedQuestion.Parameters.QuestionMark = true
	numberedQuestion.Parameters.NumberedQuestionMark = true
	braced.Parameters.Braced = true
	if sigil.Prefix != "" {
		named.Parameters.NamedSigils += sigil.Prefix[:1]
	}
	return []sqllex.Rules{dollar, question, numberedQuestion, braced, named}
}

// readsAsParameter reports whether rules read text as exactly one parameter token.
//
// Takes rules (sqllex.Rules) which are the rules.
// Takes text (string) which is a placeholder.
//
// Returns bool which is true when the text is one parameter token.
func readsAsParameter(rules sqllex.Rules, text string) bool {
	tokens, err := sqllex.Tokens(rules, text)
	return err == nil && len(tokens) == 1 && tokens[0].Kind == sqllex.KindParameter
}

// countPlaceholders reads every placeholder of a statement under the dialect's execution
// and positional forms.
//
// Takes scan (statementScan) which is the statement.
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns placeholderCount which says what they bind.
func countPlaceholders(scan statementScan, info *dialect.Info) placeholderCount {
	accepted := dialect.Placeholders{NamePrefix: info.Placeholders.NamePrefix, Execution: info.Placeholders.Execution,
		Positional: info.Placeholders.Positional, Sigils: []dialect.PlaceholderSigil{info.Placeholders.Execution, info.Placeholders.PositionalForm()}}
	count := placeholderCount{numbers: make(map[int]bool), names: make(map[string]bool), foreign: nil, forms: nil, anonymous: 0}
	for _, token := range scan.tokens {
		if token.Kind != sqllex.KindParameter {
			continue
		}
		reading, ok := accepted.Read(token.Raw)
		if !ok {
			count.foreign = append(count.foreign, token.Raw)
			continue
		}
		if !slices.Contains(count.forms, reading.Sigil) {
			count.forms = append(count.forms, reading.Sigil)
		}
		switch {
		case reading.Sigil.Kind == dialect.PlaceholderNamed:
			count.names[reading.Name] = true
		case reading.Sigil.Kind == dialect.PlaceholderNumbered && reading.Number > 0:
			count.numbers[reading.Number] = true
		default:
			count.anonymous++
		}
	}
	return count
}

// argumentProblems reports how a statement's arguments fail to match its placeholders: a
// placeholder in another form, mixed forms, numbers that skip, more or fewer arguments
// than the placeholders bind, and a named placeholder no argument is named for.
//
// Takes statement (laneStatement) which is the statement the driver received.
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns []string which describes each problem.
func argumentProblems(statement laneStatement, info *dialect.Info) []string {
	scan, err := scanStatement(statement.SQL, info)
	if err != nil {
		return []string{fmt.Sprintf("sends SQL that does not lex (%v): %q", err, statement.SQL)}
	}
	count := countPlaceholders(scan, info)
	var problems []string
	if len(count.foreign) > 0 {
		problems = append(problems, fmt.Sprintf("sends placeholders %q, which are not in the execution form %s", count.foreign, describeSigil(info.Placeholders.Execution)))
	}
	if len(count.forms) > 1 {
		problems = append(problems, "mixes placeholder forms")
	}
	bound := count.anonymous + len(count.names)
	if len(count.numbers) > 0 {
		numbers := slices.Sorted(maps.Keys(count.numbers))
		highest := slices.Max(numbers)
		if highest != len(count.numbers) {
			problems = append(problems, fmt.Sprintf("numbers its placeholders %v, skipping some", numbers))
		}
		bound += highest
	}
	if bound != len(statement.Arguments) {
		problems = append(problems, fmt.Sprintf("binds %d arguments to %d placeholders", len(statement.Arguments), bound))
	}
	for name := range count.names {
		if !slices.ContainsFunc(statement.Arguments, func(argument laneArgument) bool { return argument.Name == name }) {
			problems = append(problems, fmt.Sprintf("binds no argument named %s", name))
		}
	}
	slices.Sort(problems)
	return problems
}

// valueProblems reports each value passed to a call that reached the driver in no
// argument and no SQL.
//
// Takes call (*laneCall) which is the call.
//
// Returns []string which describes each problem.
func valueProblems(call *laneCall) []string {
	var problems []string
	for _, expected := range call.expected {
		found := false
		for _, statement := range call.outcome.Statements {
			found = found || strings.Contains(statement.SQL, expected.text) ||
				slices.ContainsFunc(statement.Arguments, func(argument laneArgument) bool { return strings.Contains(argument.Text, expected.text) })
		}
		if !found {
			problems = append(problems, fmt.Sprintf("never sent the value %s passed in %s", expected.text, expected.path))
		}
	}
	return problems
}

// terminatorOf returns the dialect's statement terminator.
//
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns string which is the terminator.
func terminatorOf(info *dialect.Info) string {
	if info.Lexical.Terminator != "" {
		return info.Lexical.Terminator
	}
	return sqllex.DefaultTerminator
}
