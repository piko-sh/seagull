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
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
)

// TokeniserFixture lets the tokeniser span check read a dialect's own tokeniser, for a
// dialect that does not tokenise with the shared lexical scanner, sqllex.Scanner.
type TokeniserFixture struct {
	// Tokens returns the tokens the dialect's tokeniser reads in a text, in order and
	// without an end-of-input token, or an error when it cannot read the text, which the
	// check then skips.
	Tokens func(source string) ([]Token, error)

	// SkipNoise advances past whitespace and comments the way the dialect's tokeniser does.
	SkipNoise NoiseSkipper
}

// lexedText is one text the tokeniser span check reads, and where it comes from.
type lexedText struct {
	// label names the text in a problem.
	label string

	// source is the text.
	source string
}

// statementSpan is where a statement starts in a text and how long it is.
type statementSpan struct {
	// start is the statement's byte offset.
	start int

	// length is the statement's length in bytes.
	length int
}

// checkTokeniserSpans compares the dialect's statement boundaries and malformed-input
// handling with sqllex. It also compares token spans when the fixture supplies a
// tokeniser.
//
// Takes fixture (*Fixture) which supplies the dialect, its SQL and its tokeniser.
//
// Returns checkOutcome which holds any problem found.
func checkTokeniserSpans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	texts := fixtureTexts(fixture)
	if len(texts) == 0 {
		return notApplicable("the fixture holds no SQL")
	}
	engine := fixture.NewDialect()
	rules := engine.Info().Lexical
	var problems []string
	for _, text := range texts {
		if problem := statementSplitProblem(ctx, engine, rules, text.source); problem != "" {
			problems = append(problems, text.label+": "+problem)
		}
		problems = append(problems, tokenSpanProblems(fixture.Tokeniser, rules, text)...)
	}
	return outcomeOf(problems)
}

// fixtureTexts lists the SQL a fixture holds: its SQL fields, its span statements and
// every SQL file of its span corpora.
//
// Takes fixture (*Fixture) which is the fixture.
//
// Returns []lexedText which holds the non-empty texts.
func fixtureTexts(fixture *Fixture) []lexedText {
	fields := []lexedText{
		{label: "BaseMigration", source: fixture.BaseMigration},
		{label: "ParamQuery", source: fixture.ParamQuery},
		{label: "SplitterMigration", source: fixture.SplitterMigration},
		{label: "QuotedMigration", source: fixture.QuotedMigration},
		{label: "ReservedWordMigration", source: fixture.ReservedWordMigration},
		{label: "ReservedWordQuery", source: fixture.ReservedWordQuery},
		{label: "TypedMigration", source: fixture.TypedMigration},
		{label: "PlaceholderQuery", source: fixture.PlaceholderQuery},
		{label: "NotInQuery", source: fixture.NotInQuery},
		{label: "GroupingQuery", source: fixture.GroupingQuery},
		{label: "UnboundStatement", source: fixture.UnboundStatement},
	}
	texts := slices.DeleteFunc(fields, func(text lexedText) bool { return text.source == "" })
	if fixture.Spans == nil {
		return texts
	}
	for index, statement := range fixture.Spans.Statements {
		texts = append(texts, lexedText{label: fmt.Sprintf("Spans.Statements[%d]", index), source: statement})
	}
	for _, files := range fixture.Spans.Corpora {
		texts = append(texts, corpusTexts(files)...)
	}
	return texts
}

// corpusTexts reads every SQL file of a corpus.
//
// Takes files (fs.FS) which holds the corpus.
//
// Returns []lexedText which holds the files' texts, labelled by their paths.
func corpusTexts(files fs.FS) []lexedText {
	var texts []lexedText
	_ = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || path.Ext(name) != sqlExtension {
			return nil
		}
		content, err := fs.ReadFile(files, name)
		if err == nil {
			texts = append(texts, lexedText{label: name, source: sqllex.TrimByteOrderMark(string(content))})
		}
		return nil
	})
	return texts
}

// statementSplitProblem compares how ParseStatements and sqllex.Split divide a text.
//
// Takes engine (dialect.DialectPort) which splits the text.
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes source (string) which is the text.
//
// Returns string which describes the difference, or is empty when they agree.
func statementSplitProblem(ctx context.Context, engine dialect.DialectPort, rules sqllex.Rules, source string) string {
	lexed, lexError := sqllex.Split(rules, source)
	parsed, parseError := engine.ParseStatements(ctx, source)
	switch {
	case lexError != nil && parseError != nil:
		if !errors.Is(parseError, sqllex.ErrMalformed) {
			return fmt.Sprintf("ParseStatements refuses the text sqllex refuses, but not as malformed: %v", parseError)
		}
		return ""
	case lexError != nil:
		return fmt.Sprintf("ParseStatements splits text sqllex refuses: %v", lexError)
	case parseError != nil:
		return fmt.Sprintf("ParseStatements refuses text sqllex splits: %v", parseError)
	}
	lexedSpans, parsedSpans := lexedStatementSpans(lexed), parsedStatementSpans(parsed)
	if !slices.Equal(lexedSpans, parsedSpans) {
		return fmt.Sprintf("the statements differ: sqllex finds (offset, length) %v, ParseStatements %v", lexedSpans, parsedSpans)
	}
	return ""
}

// lexedStatementSpans lists where sqllex's statements are.
//
// Takes statements ([]sqllex.Statement) which are the statements.
//
// Returns []statementSpan which holds their spans.
func lexedStatementSpans(statements []sqllex.Statement) []statementSpan {
	spans := make([]statementSpan, len(statements))
	for index, statement := range statements {
		spans[index] = statementSpan{start: statement.Start, length: statement.End - statement.Start}
	}
	return spans
}

// parsedStatementSpans lists where the dialect's statements are.
//
// Takes statements ([]ast.ParsedStatement) which are the statements.
//
// Returns []statementSpan which holds their spans.
func parsedStatementSpans(statements []ast.ParsedStatement) []statementSpan {
	spans := make([]statementSpan, len(statements))
	for index, statement := range statements {
		spans[index] = statementSpan{start: statement.Location, length: statement.Length}
	}
	return spans
}

// tokenSpanProblems compares the dialect's own tokeniser with sqllex over a text, when
// the fixture supplies the tokeniser and it reads the text.
//
// Takes tokeniser (*TokeniserFixture) which reads the dialect's tokens, or is nil.
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes text (lexedText) which is the text.
//
// Returns []string which describes each difference.
func tokenSpanProblems(tokeniser *TokeniserFixture, rules sqllex.Rules, text lexedText) []string {
	if tokeniser == nil || tokeniser.Tokens == nil || tokeniser.SkipNoise == nil {
		return nil
	}
	tokens, err := tokeniser.Tokens(text.source)
	if err != nil {
		return nil
	}
	differences := CompareTokeniserSpans(rules, text.source, tokens, tokeniser.SkipNoise)
	problems := make([]string, len(differences))
	for index, difference := range differences {
		problems[index] = text.label + ": " + difference
	}
	return problems
}
