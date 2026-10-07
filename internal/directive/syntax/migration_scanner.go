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

package syntax

import (
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// roleMigration is the directive role that configures a migration block.
	roleMigration = "migration"

	// roleQuery is the directive role of a query header.
	roleQuery = "query"

	// roleColumn is the directive role that overrides one column.
	roleColumn = "column"

	// keywordNonNullElements is the column directive keyword that declares a column's array
	// elements and map values non-null or nullable.
	keywordNonNullElements = "non_null_elements"

	// keywordStructTags is the column override keyword declaring the struct tags of the
	// column's generated field.
	keywordStructTags = "struct_tags"

	// maxCreateFunctionHeadTokens is the most significant tokens read from the start of a
	// statement to find the function or procedure it creates: CREATE, up to three modifiers
	// such as OR REPLACE TEMPORARY, FUNCTION or PROCEDURE, and a name of up to three dotted
	// parts.
	maxCreateFunctionHeadTokens = 10
)

// MigrationDirectives is what the directive comments of one migration file say.
type MigrationDirectives struct {
	// ReadOnlyOverrides maps the byte offset of the first significant token of each
	// statement that creates a function or procedure and directly follows a
	// sgl.migration(readonly: ...) directive to the directive's value, so the value applies
	// to that statement alone and not to other functions of the same name.
	ReadOnlyOverrides map[int]*bool

	// ColumnOverrides holds one entry per sgl.column(table.col, ...) directive, in order.
	ColumnOverrides []MigrationColumnOverride

	// Foreign holds each directive written with a foreign prefix, in order.
	Foreign []ForeignDirective

	// MisplacedQueryLines holds the 1-based lines of the sgl.query(...) directives, which
	// have no meaning in a migration.
	MisplacedQueryLines []int

	// NoTransaction is true when a sgl.migration(no_transaction: true) directive asks for
	// the migration to run without a transaction.
	NoTransaction bool
}

// MigrationColumnOverride captures the parsed shape of a single migration-level `--
// sgl.column(table.col, type: ..., go_type: ..., nullable: ...)` directive.
//
// The catalogue builder applies these to Column entries after the engine's DDL
// interpretation builds the table.
type MigrationColumnOverride struct {
	// Nullable overrides the column's nullability when set.
	Nullable *bool

	// NonNullElements declares the column's array elements and map values non-null or
	// nullable when set.
	NonNullElements *bool

	// Table is the override's target table as written in the directive, including any schema
	// qualifier and quotes.
	//
	// SplitQualifiedName(Table + "." + Column) separates the name parts. Quoted parts keep
	// their case; unquoted parts follow the dialect's identifier folding rules.
	Table string

	// Column is the column the override targets as the directive writes it, quotes included.
	Column string

	// SQLType overrides the column's SQL type when set.
	SQLType string

	// GoType overrides the column's generated Go type when set.
	GoType string

	// StructTags holds the struct_tags value without its quotes, a struct tag as Go writes
	// one, such as validate:"required", or is empty when the directive declares none.
	StructTags string

	// GoTypeSpan locates the go_type value as written, quotes included, so a diagnostic
	// about the value can point at it. Without a go_type it marks the start of the
	// directive's line.
	GoTypeSpan diagbuild.TextSpan

	// StructTagsSpan locates the struct_tags keyword argument as written, from its key to
	// the end of its value, so a diagnostic about the value can point at it. Without one it
	// marks the start of the directive's line.
	StructTagsSpan diagbuild.TextSpan
}

// ScanMigration reads the directive comments of a migration file under a dialect's
// lexicon, so a comment opener inside a string, a quoted identifier, a dollar-quoted body
// or a block comment is never taken for a directive. Each directive is read from its own
// comment line.
//
// A sgl.migration(readonly: ...) directive applies to the statement that follows it when
// that statement creates a function or procedure; any other comment between the two, and
// any other statement, drops it.
//
// It reads the directives as ParseMigration does, without the problems ParseMigration
// reports, so a malformed directive, or a malformed argument, says nothing.
//
// Takes content (string) which is the migration file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns MigrationDirectives which holds what the directives say.
func ScanMigration(content string, lexicon Lexicon, prefixes namespace.Set) MigrationDirectives {
	directives, _ := ParseMigration(content, lexicon, prefixes, "")
	return directives
}

// NoTransactionRequired checks whether a migration must run outside a transaction.
//
// It recognises explicit directives and dialect-specific statements using the engine's
// lexical rules. Unsplit text cannot be auto-detected, and a dialect predicate that
// panics reads as not required; RequiresNoTransaction returns that panic as an error.
//
// Takes content (string) which holds the migration file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns bool which is true if the migration must run without BEGIN/COMMIT.
func NoTransactionRequired(content string, lexicon Lexicon, prefixes namespace.Set) bool {
	required, err := RequiresNoTransaction(content, lexicon, prefixes)
	return err == nil && required
}

// RequiresNoTransaction checks whether a migration must run outside a transaction, as
// NoTransactionRequired does, and returns the error a dialect's predicate raised.
//
// A sgl.migration(no_transaction: true) directive requires it. Otherwise the dialect's
// lexical rules decide from the migration's statements; text the rules cannot split is
// not auto-detected, and is not an error here, since the dialect reports it when it
// parses the migration.
//
// Takes content (string) which holds the migration file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns bool which is true if the migration must run without BEGIN/COMMIT.
// Returns error which wraps sqllex.ErrRulesCallback when the dialect's
// RequiresNoTransaction predicate panicked, and is nil otherwise.
func RequiresNoTransaction(content string, lexicon Lexicon, prefixes namespace.Set) (bool, error) {
	if ScanMigration(content, lexicon, prefixes).NoTransaction {
		return true, nil
	}
	required, err := sqllex.RequiresNoTransaction(lexicon.Rules, content)
	if errors.Is(err, sqllex.ErrRulesCallback) {
		return false, fmt.Errorf("checking whether the migration must run outside a transaction: %w", err)
	}
	return err == nil && required, nil
}

// statementHead appends the significant tokens that open a statement to head, stopping at
// the statement's first opening parenthesis or its semicolon, or once head is full.
//
// Takes tokens ([]sqllex.Token) which are the content's tokens.
// Takes index (int) which is the index of the statement's first significant token.
// Takes head ([]sqllex.Token) which receives the tokens; its capacity is the most read.
//
// Returns []sqllex.Token which holds the statement's leading significant tokens.
func statementHead(tokens []sqllex.Token, index int, head []sqllex.Token) []sqllex.Token {
	for position := index; position < len(tokens) && len(head) < cap(head); position++ {
		token := tokens[position]
		if token.Kind.IsNoise() {
			continue
		}
		if token.Kind == sqllex.KindPunctuation && (token.Raw == "(" || token.Raw == ";") {
			break
		}
		head = append(head, token)
	}
	return head
}

// createdFunctionName returns the lower-case, unqualified name of the function or
// procedure a statement creates: CREATE, then OR REPLACE, TEMP or TEMPORARY, then
// FUNCTION or PROCEDURE and the name.
//
// Takes tokens ([]sqllex.Token) which are the statement's leading significant tokens, up
// to its first opening parenthesis.
//
// Returns string which is the name, or empty when the statement creates no function.
func createdFunctionName(tokens []sqllex.Token) string {
	if len(tokens) < 2 || !tokens[0].IsWord("CREATE") {
		return ""
	}
	index := 1
	for index < len(tokens) && isCreatePrefixModifier(strings.ToUpper(tokens[index].Raw)) && tokens[index].Kind == sqllex.KindWord {
		index++
	}
	if index >= len(tokens) || (!tokens[index].IsWord("FUNCTION") && !tokens[index].IsWord("PROCEDURE")) {
		return ""
	}
	return qualifiedNameTail(tokens[index+1:])
}

// qualifiedNameTail returns the last part, in lower case and without quotes, of the
// dotted name the tokens open with, such as calc for public.calc.
//
// Takes tokens ([]sqllex.Token) which open with the name.
//
// Returns string which is the name's last part, or empty when the tokens open with none.
func qualifiedNameTail(tokens []sqllex.Token) string {
	name := ""
	expectPart := true
	for _, token := range tokens {
		switch {
		case expectPart && token.Kind == sqllex.KindWord:
			name = token.Raw
		case expectPart && token.Kind == sqllex.KindQuotedIdentifier:
			name = token.Raw[1 : len(token.Raw)-1]
		case !expectPart && token.Kind == sqllex.KindPunctuation && token.Raw == ".":
		default:
			return strings.ToLower(name)
		}
		expectPart = !expectPart
	}
	return strings.ToLower(name)
}

// isCreatePrefixModifier reports whether an uppercased word is one of the modifiers that
// may appear between CREATE and the object keyword.
//
// Takes word (string) which is an uppercased statement token.
//
// Returns bool which is true when the word is a CREATE prefix modifier.
func isCreatePrefixModifier(word string) bool {
	switch word {
	case "OR", "REPLACE", "TEMP", "TEMPORARY":
		return true
	default:
		return false
	}
}
