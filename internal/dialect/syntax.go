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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// IdentifierQuote is how a dialect delimits an identifier: the text before it and the
// text after it. An identifier containing the closing text has it doubled, as SQL escapes
// a quote inside a delimited identifier.
type IdentifierQuote struct {
	// Open is the text before the identifier, such as a double quote or a backtick.
	Open string

	// Close is the text after the identifier, usually the same as Open.
	Close string
}

// Quote delimits an identifier, doubling any closing text inside it.
//
// Takes name (string) which is the raw identifier.
//
// Returns string which is the delimited identifier, or name unchanged when the quote has
// no closing text.
func (quote IdentifierQuote) Quote(name string) string {
	if quote.Close == "" {
		return name
	}
	return quote.Open + strings.ReplaceAll(name, quote.Close, quote.Close+quote.Close) + quote.Close
}

// FilterSyntax describes what the generated runtime query builder may write into a
// predicate it appends to a query, beyond what every dialect accepts.
//
// Every dialect accepts the comparisons =, <>, !=, <, >, <= and >=, LIKE, IS NULL, IS NOT
// NULL, IN and NOT IN, and a filtered column written as a plain name or with one of the
// dialect's JSON operators, which JSONOperators reads from Info.OperatorFeatures.
type FilterSyntax struct {
	// Operators lists the dialect's further comparison operators a predicate may use, as the
	// dialect spells them, such as ILIKE. The builder leaves out an operator that contains
	// the text of an anonymous placeholder, which it could not tell apart from one.
	Operators []string

	// JSONPathFunctions lists the functions that read a path from a JSON column, called as
	// function(column, 'path'), that a filtered column may be written with, such as
	// json_extract.
	JSONPathFunctions []string

	// Casts lists the forms in which a filtered column may be converted to one of CastTypes,
	// such as CAST(column AS type), which the builder accepts from its caller and writes. It
	// is empty when the builder accepts no conversion.
	Casts []CastForm

	// CastTypes lists allowed cast targets in the dialect's spelling. Matching ignores case,
	// and callers cannot supply arbitrary type text.
	CastTypes []string
}

// Affix is the text a dialect writes before and after something, such as "= ANY(" and ")"
// around an array parameter. The zero Affix writes nothing, and where a form is optional
// it means the dialect has none.
type Affix struct {
	// Before is written before the thing.
	Before string

	// After is written after the thing.
	After string
}

// ListSyntax describes how a dialect writes membership in a list of values the caller
// supplies, which code generation writes for a slice parameter.
//
// SQL has no empty list, so an empty slice is written as a test of its own, in place of
// the operator and its list together. A set with no rows serves, such as "IN (SELECT NULL
// WHERE FALSE)", on a dialect that lets such a set's column compare with any type. A
// dialect that gives the column a type of its own, as PostgreSQL types it text, writes a
// test that takes its type from the value tested instead, such as "= ANY('{}')".
type ListSyntax struct {
	// EmptyMembership is written in place of IN and its list when the slice is empty: a test
	// no row passes, whatever the type of the value tested, such as "IN (SELECT NULL WHERE
	// FALSE)" or "= ANY('{}')".
	EmptyMembership string

	// EmptyExclusion is written in place of NOT IN and its list when the slice is empty: a
	// test every row passes, whatever the type of the value tested, such as "NOT IN (SELECT
	// NULL WHERE FALSE)" or "<> ALL('{}')".
	EmptyExclusion string

	// ArrayMembership is written around one placeholder bound to the whole slice as an
	// array, in place of IN and its list, such as "= ANY(" and ")". It is zero when the
	// dialect cannot test membership of an array parameter.
	ArrayMembership Affix

	// ArrayExclusion is written around one placeholder bound to the whole slice as an array,
	// in place of NOT IN and its list, such as "<> ALL(" and ")". It is zero when
	// ArrayMembership is.
	ArrayExclusion Affix
}

// PaginationSyntax describes how a dialect writes the row limit and offset that the
// generated runtime query builder appends.
type PaginationSyntax struct {
	// Limit is written around the row limit, such as "LIMIT " and "", or "FETCH NEXT " and "
	// ROWS ONLY".
	Limit Affix

	// Offset is written around the number of rows skipped, such as "OFFSET " and "".
	Offset Affix

	// UnboundedLimit is the limit written before an offset that comes without one, for a
	// dialect that accepts an offset only after a limit, such as "-1". It is empty when an
	// offset may stand alone.
	UnboundedLimit string

	// OffsetFirst reports a dialect that writes the offset before the limit.
	OffsetFirst bool
}

// CastForm is one way a dialect writes a conversion of a value to a type: the text before
// the value, between the value and the type, and after the type, such as "CAST(", " AS "
// and ")".
type CastForm struct {
	// Before is written before the value.
	Before string

	// Middle is written between the value and the type.
	Middle string

	// After is written after the type.
	After string
}

// JSONOperators returns the operators a dialect's OperatorFeatures maps to the JSON
// expression feature, such as -> and ->>, in sorted order.
//
// Takes features (map[string]ast.ExpressionFeature) which is the dialect's
// Info.OperatorFeatures.
//
// Returns []string which holds the JSON operators, or nil when there are none.
func JSONOperators(features map[string]ast.ExpressionFeature) []string {
	var operators []string
	for operator, feature := range features {
		if feature == ast.SQLFeatureJSONOp {
			operators = append(operators, operator)
		}
	}
	slices.Sort(operators)
	return operators
}
