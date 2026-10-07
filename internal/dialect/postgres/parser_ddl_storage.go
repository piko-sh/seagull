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

package postgres

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// reloptionImpliedValue is the value a reloption written without `= value` takes, since
	// PostgreSQL treats a bare boolean option as enabled.
	reloptionImpliedValue = "true"

	// bucketCountParameter is the storage parameter that sets a hash-sharded index's bucket
	// count.
	bucketCountParameter = "bucket_count"
)

// reloption is one key = value entry of a parenthesised list of storage options.
type reloption struct {
	// key is the option's name as written, dotted when it has a namespace.
	key string

	// value is the option's value: a string's text without its quotes, or else its source
	// text; reloptionImpliedValue for a key written alone.
	value string
}

// keySharding records how the index of a primary key is sharded.
type keySharding struct {
	// buckets is the bucket_count the key's storage parameters give, as written, or empty
	// when they give none.
	buckets string

	// hashed reports that the key is hash-sharded, as USING HASH declares.
	hashed bool
}

// parseReloptions parses `( key [= value], ... )` and returns its entries in order.
//
// Values that are single-quoted strings have their quotes stripped; other values are
// captured as their source text. A key written without `= value`, such as
// `timescaledb.continuous`, is a boolean option set to true, as in PostgreSQL itself.
//
// Returns []reloption which holds the entries in the order they are written.
// Returns error when the list is malformed or a value cannot be parsed.
func (p *parser) parseReloptions() ([]reloption, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	var options []reloption
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		key, keyErr := p.parseReloptionKey()
		if keyErr != nil {
			return nil, keyErr
		}
		value, valueErr := p.parseReloptionAssignment(key)
		if valueErr != nil {
			return nil, valueErr
		}
		options = append(options, reloption{key: key, value: value})
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected ')'")
	}
	p.Advance()
	return options, nil
}

// parseReloptionKey reads a key, which may be dotted (`namespace.key`).
//
// Returns string which is the parsed key text.
// Returns error when no identifier is present or a dotted segment is missing.
func (p *parser) parseReloptionKey() (string, error) {
	if p.Current().Kind != tokenIdentifier {
		return "", dialectkit.NewParseError(p.Current(), "expected reloption key")
	}
	var builder strings.Builder
	builder.WriteString(p.Advance().Value)
	for p.Current().Kind == tokenDot {
		builder.WriteByte('.')
		p.Advance()
		if p.Current().Kind != tokenIdentifier {
			return "", dialectkit.NewParseError(p.Current(), "expected identifier after '.'")
		}
		builder.WriteString(p.Advance().Value)
	}
	return builder.String(), nil
}

// parseReloptionAssignment reads the `= value` part that follows a reloption key,
// treating a key that is directly followed by a comma or the closing paren as a boolean
// option set to true.
//
// Takes key (string) which is the reloption key, used in error messages.
//
// Returns string which is the option's value text.
// Returns error when neither `=` nor the end of the option follows the key, or the value
// cannot be parsed.
func (p *parser) parseReloptionAssignment(key string) (string, error) {
	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
		return p.parseReloptionValue()
	}
	if kind := p.Current().Kind; kind == tokenComma || kind == tokenRightParen {
		return reloptionImpliedValue, nil
	}
	return "", dialectkit.NewParseError(p.Current(), fmt.Sprintf("expected '=' after reloption key %q", key))
}

// parseReloptionValue reads a value, stopping at the next comma or closing parenthesis
// outside the groups the value opens.
//
// A single string or number is returned as its value. A multi-token value, such as
// INTERVAL '1 day', is returned as its source text exactly as written.
//
// Returns string which is the captured value text.
// Returns error when the value is missing, or a group it opens is not closed before the
// end of the statement.
func (p *parser) parseReloptionValue() (string, error) {
	first := p.Current()
	if first.Kind == tokenString || first.Kind == tokenNumber {
		p.Advance()
		return first.Value, nil
	}
	start := p.Position
	if err := p.skipToListElementEnd(); err != nil {
		return "", err
	}
	if p.Position == start {
		return "", dialectkit.NewParseError(first, "expected a value for the reloption")
	}
	return p.sourceText(start, p.Position), nil
}

// skipIndexParameters consumes the index parameters a unique, primary-key or exclusion
// constraint may carry, as parseIndexParameters reads them.
//
// Returns error when a parenthesised parameter list is unbalanced or malformed.
func (p *parser) skipIndexParameters() error {
	_, err := p.parseIndexParameters()
	return err
}

// parsePrimaryKeyIndexParameters consumes the index parameters of a primary key, as
// parseIndexParameters reads them, recording a key that USING HASH shards on the parser
// for the CREATE TABLE that declares it.
//
// Returns error when a parenthesised parameter list is unbalanced or malformed.
func (p *parser) parsePrimaryKeyIndexParameters() error {
	sharding, err := p.parseIndexParameters()
	if sharding.hashed {
		p.primaryKeySharding = sharding
	}
	return err
}

// parseIndexParameters consumes the index parameters a unique, primary-key or exclusion
// constraint may carry: INCLUDE (...), WITH (...) and USING INDEX TABLESPACE name, and
// where the flavour allows them, USING HASH and STORING (...).
//
// Returns keySharding which records whether USING HASH shards the index, and the
// bucket_count its storage parameters give.
// Returns error when a parenthesised parameter list is unbalanced or malformed.
func (p *parser) parseIndexParameters() (keySharding, error) {
	var sharding keySharding
	for {
		consumed, err := p.parseFlavourIndexOption(&sharding)
		switch {
		case err != nil:
			return sharding, err
		case consumed:
			continue
		case (p.IsKeyword("INCLUDE") || p.IsKeyword(keywordWITH)) && p.Peek().Kind == tokenLeftParen:
			p.Advance()
			if err := p.requireSkipParenthesised(); err != nil {
				return sharding, err
			}
		case p.IsKeyword(keywordUSING) && p.Peek().IsKeyword(tokenIdentifier, "INDEX"):
			p.Advance()
			p.Advance()
			p.MatchKeyword("TABLESPACE")
			if _, err := p.requireIdentifierOrKeyword(); err != nil {
				return sharding, err
			}
		default:
			return sharding, nil
		}
	}
}

// parseFlavourIndexOption consumes an index option a flavour allows on a primary key or
// unique constraint: USING HASH, which shards the index, STORING (...), which lists the
// columns the index stores, and WITH (...), whose bucket_count sets a sharded index's
// buckets.
//
// Takes sharding (*keySharding) which records the sharding the option declares.
//
// Returns bool which is true when an option was consumed.
// Returns error when a STORING or WITH list is unbalanced or malformed.
func (p *parser) parseFlavourIndexOption(sharding *keySharding) (bool, error) {
	switch {
	case !p.indexOptions:
		return false, nil
	case p.IsKeyword(keywordUSING) && p.Peek().IsKeyword(tokenIdentifier, "HASH"):
		p.Advance()
		p.Advance()
		sharding.hashed = true
		return true, nil
	case p.IsKeyword("STORING") && p.Peek().Kind == tokenLeftParen:
		p.Advance()
		return true, p.requireSkipParenthesised()
	case p.IsKeyword(keywordWITH) && p.Peek().Kind == tokenLeftParen:
		p.Advance()
		options, err := p.parseReloptions()
		if err != nil {
			return true, p.recordSyntaxError(err)
		}
		for _, option := range options {
			if strings.EqualFold(option.key, bucketCountParameter) {
				sharding.buckets = option.value
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

// parseTrailingTableClauses consumes the clauses after the element list and INHERITS
// clause of a CREATE TABLE, such as PARTITION BY, USING, ON COMMIT, TABLESPACE or a
// foreign table's SERVER and OPTIONS, and reads the storage parameters its WITH (...)
// clauses give.
//
// Returns []reloption which holds the storage parameters in the order they are written.
// Returns error when a WITH list is malformed or a parenthesis is unbalanced.
func (p *parser) parseTrailingTableClauses() ([]reloption, error) {
	var parameters []reloption
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon {
		switch {
		case p.IsKeyword(keywordWITH) && p.Peek().Kind == tokenLeftParen:
			p.Advance()
			options, err := p.parseReloptions()
			if err != nil {
				return nil, p.recordSyntaxError(err)
			}
			parameters = append(parameters, options...)
		case p.Current().Kind == tokenLeftParen:
			if err := p.requireSkipParenthesised(); err != nil {
				return nil, err
			}
		default:
			p.Advance()
		}
	}
	return parameters, nil
}

// skipStorageClauses consumes the clauses a CREATE TABLE ... AS, CREATE VIEW or CREATE
// MATERIALIZED VIEW may carry before AS: USING method, WITH (...) or WITHOUT OIDS, ON
// COMMIT and TABLESPACE name.
//
// Returns error when a WITH list is unbalanced or a clause misses its name or action.
func (p *parser) skipStorageClauses() error {
	for {
		switch {
		case p.IsKeyword(keywordWITH) && p.Peek().Kind == tokenLeftParen:
			p.Advance()
			if err := p.requireSkipParenthesised(); err != nil {
				return err
			}
		case p.IsKeyword("WITHOUT") && p.Peek().IsKeyword(tokenIdentifier, "OIDS"):
			p.Advance()
			p.Advance()
		case p.IsKeyword(keywordON) && p.Peek().IsKeyword(tokenIdentifier, "COMMIT"):
			p.Advance()
			p.Advance()
			if err := p.requireKeyword("PRESERVE", "DELETE", keywordDROP); err != nil {
				return err
			}
			p.MatchKeyword("ROWS")
		case p.MatchKeyword(keywordUSING), p.MatchKeyword("TABLESPACE"):
			if _, err := p.requireIdentifierOrKeyword(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}
