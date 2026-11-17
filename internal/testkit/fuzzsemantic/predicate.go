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

package fuzzsemantic

import (
	"strings"
)

// truth is a three-valued SQL condition result.
type truth uint8

const (
	// truthFalse is FALSE.
	truthFalse truth = iota

	// truthTrue is TRUE.
	truthTrue

	// truthUnknown is NULL.
	truthUnknown
)

// parameterMark stands for the predicate's parameter in its template.
const parameterMark = "{K}"

// comparison names an integer comparison.
type comparison uint8

const (
	// equal is =.
	equal comparison = iota

	// different is <>.
	different

	// less is <.
	less

	// lessOrEqual is <=.
	lessOrEqual

	// greater is >.
	greater

	// greaterOrEqual is >=.
	greaterOrEqual
)

// predicate is one partitioning condition over the item alias i.
type predicate struct {
	// evaluate gives the condition's value for one item under the engine's rules.
	evaluate func(c *Case, item Item) truth

	// template is the SQL with parameterMark where the parameter goes.
	template string
}

// predicates lists the partitioning conditions in a stable order that inputs index.
var predicates = []predicate{
	{template: "i.a > {K}", evaluate: func(c *Case, item Item) truth { return compare(item.A, new(c.Parameter), greater) }},
	{template: "i.a + i.b < {K}", evaluate: func(c *Case, item Item) truth { return compare(add(item.A, item.B), new(c.Parameter), less) }},
	{template: "COALESCE(i.a, i.b) = {K}", evaluate: func(c *Case, item Item) truth {
		return compare(coalesce(item.A, item.B), new(c.Parameter), equal)
	}},
	{template: "i.a BETWEEN {K} AND i.b", evaluate: func(c *Case, item Item) truth {
		return and(compare(item.A, new(c.Parameter), greaterOrEqual), compare(item.A, item.B, lessOrEqual))
	}},
	{template: "NOT (i.b <> {K}) OR i.a IS NULL", evaluate: func(c *Case, item Item) truth {
		return or(not(compare(item.B, new(c.Parameter), different)), known(item.A == nil))
	}},
	{template: "CASE WHEN i.a > i.b THEN i.a ELSE i.b END >= {K}", evaluate: func(c *Case, item Item) truth {
		larger := item.B
		if compare(item.A, item.B, greater) == truthTrue {
			larger = item.A
		}
		return compare(larger, new(c.Parameter), greaterOrEqual)
	}},
	{template: "i.a IN ({K}, 1, -1)", evaluate: func(c *Case, item Item) truth {
		return member(item.A, []*int64{new(c.Parameter), new(int64(1)), new(int64(-1))}, false)
	}},
	{template: "(i.a > {K}) <> (i.b > 0)", evaluate: func(c *Case, item Item) truth {
		return compareTruth(compare(item.A, new(c.Parameter), greater), compare(item.B, new(int64(0)), greater))
	}},
	{template: "i.a % 2 = {K}", evaluate: func(c *Case, item Item) truth {
		if item.A == nil {
			return truthUnknown
		}
		return compare(new(*item.A%2), new(c.Parameter), equal)
	}},
	{template: "i.a < (SELECT MAX(p.c) FROM " + peerTable + " p) + {K}", evaluate: func(c *Case, item Item) truth {
		return compare(item.A, add(c.maximumC(), new(c.Parameter)), less)
	}},
	{template: "i.label LIKE 'a%' AND i.grp <> {K}", evaluate: func(c *Case, item Item) truth {
		like := truthUnknown
		if item.Label != nil {
			like = known(strings.HasPrefix(*item.Label, "a"))
		}
		return and(like, compare(new(item.Group), new(c.Parameter), different))
	}},
	{template: "ABS(i.a - {K}) <= i.b", evaluate: func(c *Case, item Item) truth {
		difference := add(item.A, new(-c.Parameter))
		if difference != nil && *difference < 0 {
			difference = new(-*difference)
		}
		return compare(difference, item.B, lessOrEqual)
	}},
	{template: "i.id IN (SELECT p.item_id FROM " + peerTable + " p WHERE p.c >= {K})", evaluate: func(c *Case, item Item) truth {
		var references []*int64
		for _, peer := range c.Peers {
			if compare(peer.C, new(c.Parameter), greaterOrEqual) == truthTrue {
				references = append(references, peer.ItemID)
			}
		}
		return member(new(item.ID), references, c.Engine == engineClickHouse)
	}},
}

// sql writes the predicate with a parameter placeholder.
//
// Takes placeholder (string) which spells the parameter.
//
// Returns string which is the condition.
func (p predicate) sql(placeholder string) string {
	return strings.ReplaceAll(p.template, parameterMark, placeholder)
}

// maximumC is the largest non-NULL peer value.
//
// Returns *int64 which is nil when no peer has a value.
func (c *Case) maximumC() *int64 {
	var result *int64
	for _, peer := range c.Peers {
		if peer.C != nil && (result == nil || *peer.C > *result) {
			result = new(*peer.C)
		}
	}
	return result
}

// truthOf evaluates the case's predicate for one item.
//
// Takes item (Item) which is the row.
//
// Returns truth which is the predicate's value.
func (c *Case) truthOf(item Item) truth {
	return predicates[c.Predicate].evaluate(c, item)
}

// compare applies a comparison with NULL propagation.
//
// Takes left (*int64) which is nil for NULL.
// Takes right (*int64) which is nil for NULL.
// Takes operator (comparison) which selects the comparison.
//
// Returns truth which is unknown when either side is NULL.
func compare(left, right *int64, operator comparison) truth {
	if left == nil || right == nil {
		return truthUnknown
	}
	switch operator {
	case equal:
		return known(*left == *right)
	case different:
		return known(*left != *right)
	case less:
		return known(*left < *right)
	case lessOrEqual:
		return known(*left <= *right)
	case greater:
		return known(*left > *right)
	default:
		return known(*left >= *right)
	}
}

// compareTruth applies <> to two conditions, as engines do with Boolean values.
//
// Takes left (truth) which is the first condition.
// Takes right (truth) which is the second condition.
//
// Returns truth which is unknown when either side is unknown.
func compareTruth(left, right truth) truth {
	if left == truthUnknown || right == truthUnknown {
		return truthUnknown
	}
	return known(left != right)
}

// member applies IN to a value and a list.
//
// Takes value (*int64) which is nil for NULL.
// Takes list ([]*int64) which may hold NULL.
// Takes ignoreNull (bool) which applies ClickHouse's default rule that a NULL list
// element never makes a non-NULL value's membership unknown.
//
// Returns truth which is true on a match, unknown when the value or, outside the
// ClickHouse rule, an unmatched list element is NULL, and false otherwise.
func member(value *int64, list []*int64, ignoreNull bool) truth {
	if value == nil {
		if len(list) == 0 {
			return truthFalse
		}
		return truthUnknown
	}
	result := truthFalse
	for _, element := range list {
		switch {
		case element == nil:
			if !ignoreNull {
				result = truthUnknown
			}
		case *element == *value:
			return truthTrue
		}
	}
	return result
}

// and applies three-valued conjunction.
//
// Takes left (truth) which is the first operand.
// Takes right (truth) which is the second operand.
//
// Returns truth which is false when either is false.
func and(left, right truth) truth {
	switch {
	case left == truthFalse || right == truthFalse:
		return truthFalse
	case left == truthTrue && right == truthTrue:
		return truthTrue
	default:
		return truthUnknown
	}
}

// or applies three-valued disjunction.
//
// Takes left (truth) which is the first operand.
// Takes right (truth) which is the second operand.
//
// Returns truth which is true when either is true.
func or(left, right truth) truth {
	return not(and(not(left), not(right)))
}

// not applies three-valued negation.
//
// Takes value (truth) which is the operand.
//
// Returns truth which keeps unknown.
func not(value truth) truth {
	switch value {
	case truthTrue:
		return truthFalse
	case truthFalse:
		return truthTrue
	default:
		return truthUnknown
	}
}

// known converts a Go Boolean.
//
// Takes value (bool) which is the condition.
//
// Returns truth which is true or false.
func known(value bool) truth {
	if value {
		return truthTrue
	}
	return truthFalse
}

// add applies + with NULL propagation.
//
// Takes left (*int64) which is nil for NULL.
// Takes right (*int64) which is nil for NULL.
//
// Returns *int64 which is nil when either side is NULL.
func add(left, right *int64) *int64 {
	if left == nil || right == nil {
		return nil
	}
	return new(*left + *right)
}

// coalesce returns the first non-NULL value.
//
// Takes first (*int64) which is nil for NULL.
// Takes second (*int64) which is nil for NULL.
//
// Returns *int64 which is nil when both are NULL.
func coalesce(first, second *int64) *int64 {
	if first != nil {
		return first
	}
	return second
}
