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

package sqltemplate

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// unmatched marks an occurrence not yet matched to a parameter.
	unmatched = -1

	// clauseCount is how many clauses a dynamic query edits: the filter, ORDER BY and
	// pagination.
	clauseCount = 3
)

// occurrence is one placeholder of the statement, matched to the parameter it binds.
type occurrence struct {
	// sigil is the form the placeholder is written in.
	sigil dialect.PlaceholderSigil

	// span covers the placeholder in Input.SQL.
	span ast.Span

	// typeSpan covers the type a typed form writes, and is zero otherwise.
	typeSpan ast.Span

	// operator covers the IN, or NOT IN, of the list the placeholder is the sole element of,
	// and is zero when it is no such element.
	operator ast.Span

	// list covers that list from its opening parenthesis through its closing one.
	list ast.Span

	// parameter is the index into the query's Parameters of the parameter it binds.
	parameter int

	// ordinal is the placeholder's position among the statement's anonymous ones, counting
	// from one, for an anonymous form.
	ordinal int

	// number is the parameter number a numbered form carries, as the dialect recorded it or,
	// when it recorded none, as the placeholder writes it.
	number int

	// negated reports a list written NOT IN.
	negated bool
}

// parameterIndex finds the parameter a placeholder binds, by the placeholder's name or
// number.
type parameterIndex struct {
	// byName maps each placeholder identifier, or name when a parameter has none, to the
	// first parameter that carries it.
	byName map[string]int

	// byNumber maps each positive parameter number to the first parameter that carries it.
	byNumber map[int]int
}

// newParameterIndex indexes the query's parameters by placeholder name and number.
//
// Takes parameters ([]sqlmodel.QueryParameter) which are the query's parameters.
//
// Returns parameterIndex which finds them.
func newParameterIndex(parameters []sqlmodel.QueryParameter) parameterIndex {
	index := parameterIndex{byName: make(map[string]int, len(parameters)), byNumber: make(map[int]int, len(parameters))}
	for position := range parameters {
		identifier := parameters[position].Placeholder
		if identifier == "" {
			identifier = parameters[position].Name
		}
		if _, seen := index.byName[identifier]; identifier != "" && !seen {
			index.byName[identifier] = position
		}
		if _, seen := index.byNumber[parameters[position].Number]; parameters[position].Number > 0 && !seen {
			index.byNumber[parameters[position].Number] = position
		}
	}
	return index
}

// named returns the parameter a named placeholder binds: the first whose placeholder
// identifier, or name when it has none, is the placeholder's name.
//
// Takes name (string) which is the placeholder's name.
//
// Returns int which is the parameter's index, or unmatched.
func (index parameterIndex) named(name string) int {
	if position, found := index.byName[name]; found && name != "" {
		return position
	}
	return unmatched
}

// numbered returns the parameter a numbered placeholder binds.
//
// Takes number (int) which is the placeholder's number.
//
// Returns int which is the parameter's index, or unmatched.
func (index parameterIndex) numbered(number int) int {
	if position, found := index.byNumber[number]; found && number > 0 {
		return position
	}
	return unmatched
}

// matchPlaceholders matches SQL placeholders to their declared parameters.
//
// It uses each form's number, name or position and reports inconsistent spans or
// unmatched placeholders as broken invariants.
//
// Returns bool which is false when an invariant is broken.
func (b *builder) matchPlaceholders() bool {
	parameters := b.input.Query.Parameters
	index := newParameterIndex(parameters)
	occurrences := make([]occurrence, 0, len(b.spans.Placeholders))
	named := make(map[int]bool)
	anonymous := 0
	for _, placeholder := range b.spans.Placeholders {
		current, inside := b.occurrenceOf(placeholder)
		if !inside {
			b.report(diag.CodeInternalNilGuard, ast.Span{}, "the dialect located a placeholder outside the query's statement")
			return false
		}
		switch current.sigil.Kind {
		case dialect.PlaceholderNamed:
			current.parameter = index.named(placeholder.Name)
			if current.parameter != unmatched {
				named[current.parameter] = true
			}
		case dialect.PlaceholderAnonymous:
			anonymous++
			current.ordinal = cmpOr(placeholder.Number, anonymous)
		default:
			current.parameter = index.numbered(current.number)
		}
		occurrences = append(occurrences, current)
	}
	matchAnonymous(occurrences, positionalParameters(len(parameters), named))
	for position := range occurrences {
		if occurrences[position].parameter == unmatched {
			b.report(diag.CodeInternalNilGuard, occurrences[position].span, "the placeholder %s matches none of the query's parameters",
				b.input.SQL[occurrences[position].span.Start:occurrences[position].span.End])
			return false
		}
	}
	b.occurrences = occurrences
	return b.checkCoverage()
}

// occurrenceOf reads one placeholder span: where it is, the form it is written in, and
// the list it is the sole element of.
//
// Takes placeholder (ast.PlaceholderSpan) which is the span the dialect recorded.
//
// Returns occurrence which describes the placeholder, not yet matched.
// Returns bool which is false when the placeholder lies outside Input.SQL.
func (b *builder) occurrenceOf(placeholder ast.PlaceholderSpan) (occurrence, bool) {
	current := occurrence{
		sigil:     dialect.PlaceholderSigil{},
		span:      b.absolute(placeholder.Span),
		typeSpan:  b.absolute(placeholder.Type),
		operator:  b.absolute(placeholder.List.Operator),
		list:      b.absolute(placeholder.List.List),
		parameter: unmatched,
		ordinal:   0,
		number:    placeholder.Number,
		negated:   placeholder.List.Negated,
	}
	if !current.span.Known() || current.span.End > len(b.input.SQL) || current.list.End > len(b.input.SQL) ||
		current.typeSpan.End > len(b.input.SQL) {
		return current, false
	}
	text := b.input.SQL[current.span.Start:current.span.End]
	reading, read := b.input.Info.Placeholders.Read(text)
	switch {
	case read:
		current.sigil = reading.Sigil
		current.number = cmpOr(current.number, reading.Number)
	case placeholder.Name != "":
		current.sigil.Kind = dialect.PlaceholderNamed
	default:
		current.sigil.Kind = dialect.PlaceholderNumbered
	}
	return current, true
}

// checkCoverage reports, as a broken invariant, a placeholder token of the statement the
// dialect did not locate, which the template would leave as text unbound.
//
// Returns bool which is false when such a token exists.
func (b *builder) checkCoverage() bool {
	located := make(map[ast.Span]bool, len(b.occurrences))
	for index := range b.occurrences {
		located[b.occurrences[index].span] = true
	}
	for _, token := range b.tokens {
		span := ast.Span{Start: token.Start, End: token.End}
		if token.Kind == sqllex.KindParameter && !located[span] {
			b.report(diag.CodeInternalNilGuard, span, "the dialect did not locate the placeholder %s", token.Raw)
			return false
		}
	}
	return true
}

// checkPrecedingStatements reports, once for each statement before the query's own, the
// placeholders it holds, which the generated code binds no value to: the first of them,
// and how many more there are.
func (b *builder) checkPrecedingStatements() {
	statements := b.input.Statements
	for _, statement := range statements[:len(statements)-1] {
		var first sqllex.Token
		count := 0
		for _, token := range b.significantTokens(statement) {
			if token.Kind != sqllex.KindParameter {
				continue
			}
			if count == 0 {
				first = token
			}
			count++
		}
		b.reportPrecedingPlaceholders(first, count)
	}
}

// reportPrecedingPlaceholders reports the placeholders of a statement before the query's
// own, at the first of them.
//
// Takes first (sqllex.Token) which is the statement's first placeholder.
// Takes count (int) which is how many placeholders the statement holds.
func (b *builder) reportPrecedingPlaceholders(first sqllex.Token, count int) {
	const advice = ", which the generated code binds no value to; bind parameters only in the query's statement"
	span := ast.Span{Start: first.Start, End: first.End}
	switch count {
	case 0:
	case 1:
		b.report(diag.CodeParametersBeforeStatement, span, "the placeholder %s is in a statement before the query's own"+advice, first.Raw)
	default:
		b.report(diag.CodeParametersBeforeStatement, span, "the placeholder %s and %d more are in a statement before the query's own"+advice,
			first.Raw, count-1)
	}
}

// checkParameterShapes reports, once per parameter, a placeholder whose parameter's
// qualities do not fit where it is written, and each parameter in the ORDER BY a sortable
// query replaces.
func (b *builder) checkParameterShapes() {
	reported := make(map[int]bool)
	for index := range b.occurrences {
		current := &b.occurrences[index]
		if !reported[current.parameter] && b.checkShape(current) {
			reported[current.parameter] = true
		}
	}
	b.checkSortableOrderBy()
}

// checkShape reports a slice parameter whose type is an array already or whose
// placeholder is not the sole element of an IN list, and an optional parameter whose
// placeholder carries a type that cannot hold NULL.
//
// Takes current (*occurrence) which is the placeholder.
//
// Returns bool which is true when a problem was reported.
func (b *builder) checkShape(current *occurrence) bool {
	parameter := &b.input.Query.Parameters[current.parameter]
	text := b.input.SQL[current.span.Start:current.span.End]
	switch {
	case parameter.IsSlice && parameter.SQLType.Category == sqlmodel.TypeCategoryArray:
		b.report(diag.CodeSliceParameterShape, current.span,
			"parameter %q is kind: slice, but its type %s is an array already, so each element would be a list; bind the array "+
				"as one value without kind: slice", parameter.Name, typeName(parameter.SQLType))
	case parameter.IsSlice && !current.operator.Known():
		b.report(diag.CodeSliceParameterShape, current.span,
			"parameter %q is kind: slice, but its placeholder %s is not the only element of an IN list, so the slice's elements "+
				"have nowhere to go; write it alone in the list, as in IN (%s)", parameter.Name, text, text)
	case parameter.IsOptional && current.sigil.TypeSeparator != "" && current.typeSpan.Known() && !parameter.SQLType.Nullable:
		b.report(diag.CodeOptionalNotNullable, current.span,
			"parameter %q is optional: true, but its placeholder %s gives it the type %s, which cannot hold NULL, so a nil "+
				"value matches no row; give the placeholder a type that holds NULL", parameter.Name, text,
			b.input.SQL[current.typeSpan.Start:current.typeSpan.End])
	default:
		return false
	}
	return true
}

// checkSortableOrderBy reports each parameter other than the sortable one written in the
// ORDER BY of a sortable query, which the generated code drops with the clause it
// replaces.
func (b *builder) checkSortableOrderBy() {
	orderBy := b.absolute(b.spans.OrderBy)
	if !orderBy.Known() || !hasSortable(b.input.Query.Parameters) {
		return
	}
	for index := range b.occurrences {
		current := &b.occurrences[index]
		parameter := &b.input.Query.Parameters[current.parameter]
		if parameter.Kind != sqlmodel.ParameterDirectiveSortable && orderBy.Contains(current.span) {
			b.report(diag.CodeSortableOrderByParameter, current.span,
				"parameter %q is in the ORDER BY of a sortable query, which the generated code replaces with the caller's "+
					"ordering, so it would never be bound; move it out of the ORDER BY", parameter.Name)
		}
	}
}

// positionalParameters lists, in order, the parameters no named placeholder binds, which
// the anonymous placeholders bind in turn.
//
// Takes count (int) which is how many parameters the query has.
// Takes named (map[int]bool) which holds the parameters a named placeholder binds.
//
// Returns []int which holds the indices of the positional parameters.
func positionalParameters(count int, named map[int]bool) []int {
	positional := make([]int, 0, count)
	for index := range count {
		if !named[index] {
			positional = append(positional, index)
		}
	}
	return positional
}

// matchAnonymous matches each anonymous placeholder to the positional parameter at its
// ordinal.
//
// Takes occurrences ([]occurrence) which holds the placeholders; it is updated in place.
// Takes positional ([]int) which lists the positional parameters in order.
func matchAnonymous(occurrences []occurrence, positional []int) {
	for index := range occurrences {
		ordinal := occurrences[index].ordinal
		if occurrences[index].sigil.Kind == dialect.PlaceholderAnonymous && ordinal > 0 && ordinal <= len(positional) {
			occurrences[index].parameter = positional[ordinal-1]
		}
	}
}

// hasSortable reports whether a query has a sortable parameter.
//
// Takes parameters ([]sqlmodel.QueryParameter) which are the query's parameters.
//
// Returns bool which is true when one is sortable.
func hasSortable(parameters []sqlmodel.QueryParameter) bool {
	for index := range parameters {
		if parameters[index].Kind == sqlmodel.ParameterDirectiveSortable {
			return true
		}
	}
	return false
}

// typeName returns how a message names a type: its name in the engine, or its category
// when it has none.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns string which is the name.
func typeName(sqlType sqlmodel.SQLType) string {
	if sqlType.EngineName != "" {
		return sqlType.EngineName
	}
	return sqlType.Category.String()
}

// cmpOr returns the first of two numbers that is positive.
//
// Takes preferred (int) which is returned when positive.
// Takes fallback (int) which is returned otherwise.
//
// Returns int which is the number.
func cmpOr(preferred, fallback int) int {
	if preferred > 0 {
		return preferred
	}
	return fallback
}
