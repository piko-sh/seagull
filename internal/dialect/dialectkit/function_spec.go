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

package dialectkit

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// specFlagNullOnNull marks a signature whose result is NULL when any argument is NULL.
	specFlagNullOnNull = "null-on-null"

	// specFlagNeverNull marks a signature that never returns NULL.
	specFlagNeverNull = "never-null"

	// specFlagCalledOnNull marks a signature that is called even with NULL arguments.
	specFlagCalledOnNull = "called-on-null"

	// specFlagNullingArgument names the zero-based argument whose NULL alone makes the
	// result NULL.
	specFlagNullingArgument = "nulling-argument"

	// specFlagAggregate marks an aggregate.
	specFlagAggregate = "aggregate"

	// specFlagVariadic marks a signature whose last argument repeats and gives its minimum
	// argument count.
	specFlagVariadic = "variadic"

	// specFlagMinArguments gives the minimum argument count of a signature whose trailing
	// arguments are optional.
	specFlagMinArguments = "min-arguments"

	// specFlagReturnsSet marks a set-returning signature.
	specFlagReturnsSet = "returns-set"

	// specFlagReturnTypeFrom names the one-based argument whose type the result takes.
	specFlagReturnTypeFrom = "return-type-from"

	// specFlagModifiesData marks a signature that may modify data.
	specFlagModifiesData = "modifies-data"

	// specFlagSchema names the schema the signature belongs to.
	specFlagSchema = "schema"

	// specMarkerOptional ends an argument that has a default and may be left out.
	specMarkerOptional = '?'

	// specMarkerNotNull ends an argument that must not be NULL.
	specMarkerNotNull = '!'
)

// FunctionSpec is a built-in function catalogue written as text and decoded once, which
// Register adds to a catalogue builder.
//
// A dialect embeds its spec, written in its own type spelling, decodes it with
// ParseFunctionSpec the first time a catalogue is built, and supplies its own facts
// through FunctionSpecHooks: how a type spelling resolves, the NULL behaviour a line that
// names none takes, and where a signature is filed. The text carries no engine facts of
// its own beyond what each line says.
//
// The text holds one signature per line, in registration order. Blank lines and lines
// starting with # are ignored. A signature line reads
//
//	name(argument, ...) return-type flag ...
//
// where each argument is a type spelling, optionally preceded by the argument's name, and
// optionally followed by ? when it has a default and may be left out and ! when it must
// not be NULL, as in round(x decimal, places integer?) decimal. Commas, spaces, ? and !
// inside parentheses, square brackets or single quotes belong to the type spelling, so a
// spelling such as map(text, integer) or decimal(18, 3) needs no quoting; a spelling must
// not hold a space outside them.
//
// The flags are:
//
//   - null-on-null, never-null or called-on-null: the NULL behaviour, overriding the
//     dialect's default;
//   - nulling-argument=N: the result is NULL only when the zero-based argument N is NULL;
//   - aggregate: the signature is an aggregate;
//   - variadic=N: the last argument repeats, and a call passes at least N arguments;
//   - min-arguments=N: a call passes at least N arguments;
//   - returns-set: the signature returns a set of rows;
//   - return-type-from=N: the result takes the type of the one-based argument N;
//   - modifies-data: the signature may modify data, rather than only read it;
//   - schema=NAME: the signature belongs to the schema NAME.
type FunctionSpec struct {
	// spellings holds each distinct type spelling once, in order of first use; a signature
	// names its types by index into it.
	spellings []string

	// signatures holds the decoded signatures in line order.
	signatures []specSignature
}

// Register adds every signature in the spec through hooks.Add, in line order.
//
// Each registration creates fresh signatures and argument slices. Their SQL types come
// from hooks.ResolveType, which controls any sharing within those types. A nil spec
// registers nothing.
//
// Takes hooks (FunctionSpecHooks) which resolve the type spellings, give the default NULL
// behaviour and file each signature.
func (s *FunctionSpec) Register(hooks FunctionSpecHooks) {
	if s == nil {
		return
	}
	types := make([]sqlmodel.SQLType, len(s.spellings))
	for index, spelling := range s.spellings {
		types[index] = hooks.ResolveType(spelling)
	}
	for index := range s.signatures {
		entry := &s.signatures[index]
		registered := hooks.Add(entry.name, entry.function(types, hooks.DefaultNullability))
		if registered == nil {
			continue
		}
		if entry.modifiesData {
			registered.DataAccess = sqlmodel.DataAccessModifiesData
		}
		if entry.schema != "" {
			registered.Schema = entry.schema
		}
	}
}

// spellingIndex returns the index of a type spelling, adding it on first use.
//
// Takes indexes (map[string]int) which maps each spelling seen so far to its index.
// Takes spelling (string) which is the type spelling.
//
// Returns int which is the spelling's index into the spec's spellings.
func (s *FunctionSpec) spellingIndex(indexes map[string]int, spelling string) int {
	if index, found := indexes[spelling]; found {
		return index
	}
	index := len(s.spellings)
	s.spellings = append(s.spellings, spelling)
	indexes[spelling] = index
	return index
}

// FunctionSpecHooks supplies the dialect's own facts when a FunctionSpec is registered.
type FunctionSpecHooks struct {
	// ResolveType returns the type a spelling in the spec names, resolved by the dialect's
	// own type table or normaliser. Each distinct spelling is resolved once per Register
	// call.
	ResolveType func(spelling string) sqlmodel.SQLType

	// DefaultNullability returns the NULL behaviour of a signature whose line names none,
	// given its name and whether it is an aggregate. When nil, such a signature keeps the
	// zero FunctionNullableBehaviour.
	DefaultNullability func(name string, aggregate bool) sqlmodel.FunctionNullableBehaviour

	// Add files a signature under its name and returns the registered signature, such as
	// CatalogueBuilder.Add.
	Add func(name string, signature *sqlmodel.Function) *sqlmodel.Function
}

// specSignature is one decoded signature line.
type specSignature struct {
	// name is the function name.
	name string

	// schema is the schema the signature belongs to, or empty.
	schema string

	// arguments holds the arguments in order.
	arguments []specArgument

	// returnType is the index of the result type's spelling.
	returnType int

	// nullingArgument is the zero-based argument whose NULL alone makes the result NULL,
	// read when nullability is FunctionNullableNullWhenArgumentNull.
	nullingArgument int

	// minArguments is the minimum argument count, or zero.
	minArguments int

	// returnTypeFromArgument is the one-based argument whose type the result takes, or zero.
	returnTypeFromArgument int

	// nullability is the NULL behaviour the line names, read when hasNullability is set.
	nullability sqlmodel.FunctionNullableBehaviour

	// hasNullability reports whether the line names the NULL behaviour.
	hasNullability bool

	// aggregate reports an aggregate.
	aggregate bool

	// variadic reports a signature whose last argument repeats.
	variadic bool

	// returnsSet reports a set-returning signature.
	returnsSet bool

	// modifiesData reports a signature that may modify data.
	modifiesData bool
}

// function builds the signature a line describes.
//
// Takes types ([]sqlmodel.SQLType) which holds the resolved type of each spelling, by
// index.
// Takes defaultNullability (func(string, bool) sqlmodel.FunctionNullableBehaviour) which
// gives the NULL behaviour when the line names none, or is nil.
//
// Returns *sqlmodel.Function which is the signature, not yet registered.
func (s *specSignature) function(
	types []sqlmodel.SQLType,
	defaultNullability func(string, bool) sqlmodel.FunctionNullableBehaviour,
) *sqlmodel.Function {
	function := &sqlmodel.Function{}
	function.Name = s.name
	function.ReturnType = types[s.returnType]
	if len(s.arguments) > 0 {
		function.Arguments = make([]sqlmodel.FunctionArgument, len(s.arguments))
		for index := range s.arguments {
			argument := &s.arguments[index]
			function.Arguments[index] = sqlmodel.FunctionArgument{
				Name:       argument.name,
				Type:       types[argument.typeIndex],
				IsOptional: argument.optional,
				NotNull:    argument.notNull,
			}
		}
	}
	switch {
	case s.hasNullability:
		function.NullableBehaviour = s.nullability
		function.NullingArgument = s.nullingArgument
	case defaultNullability != nil:
		function.NullableBehaviour = defaultNullability(s.name, s.aggregate)
	}
	function.IsAggregate = s.aggregate
	function.IsVariadic = s.variadic
	function.MinArguments = s.minArguments
	function.ReturnsSet = s.returnsSet
	function.ReturnTypeFromArgument = s.returnTypeFromArgument
	return function
}

// applyFlag records one flag on the signature.
//
// Takes flag (string) which is the flag, with its =value when it takes one.
//
// Returns error when the flag is unknown, repeats a NULL behaviour, or has a malformed
// value.
func (s *specSignature) applyFlag(flag string) error {
	key, value, hasValue := strings.Cut(flag, "=")
	switch key {
	case specFlagNullOnNull, specFlagNeverNull, specFlagCalledOnNull:
		return s.setNullability(flag, specNullability(key), hasValue)
	case specFlagNullingArgument:
		position, err := specCount(flag, value, hasValue)
		if err != nil {
			return err
		}
		s.nullingArgument = position
		return s.setNullability(flag, sqlmodel.FunctionNullableNullWhenArgumentNull, false)
	case specFlagVariadic:
		minimum, err := specCount(flag, value, hasValue)
		s.variadic = true
		s.minArguments = minimum
		return err
	case specFlagMinArguments:
		minimum, err := specCount(flag, value, hasValue)
		s.minArguments = minimum
		return err
	case specFlagReturnTypeFrom:
		position, err := specCount(flag, value, hasValue)
		s.returnTypeFromArgument = position
		return err
	case specFlagSchema:
		if !hasValue || value == "" {
			return fmt.Errorf("flag %q needs a schema name", flag)
		}
		s.schema = value
		return nil
	}
	return s.applySwitch(flag, hasValue)
}

// applySwitch records a flag that takes no value.
//
// Takes flag (string) which is the flag.
// Takes hasValue (bool) which is true when the flag was given a value.
//
// Returns error when the flag is unknown or was given a value.
func (s *specSignature) applySwitch(flag string, hasValue bool) error {
	switch {
	case hasValue:
		return fmt.Errorf("unknown flag %q", flag)
	case flag == specFlagAggregate:
		s.aggregate = true
	case flag == specFlagReturnsSet:
		s.returnsSet = true
	case flag == specFlagModifiesData:
		s.modifiesData = true
	default:
		return fmt.Errorf("unknown flag %q", flag)
	}
	return nil
}

// setNullability records the line's NULL behaviour, once.
//
// Takes flag (string) which is the flag naming it, for the error.
// Takes nullability (sqlmodel.FunctionNullableBehaviour) which is the behaviour.
// Takes hasValue (bool) which is true when the flag was wrongly given a value.
//
// Returns error when the line already named a NULL behaviour or the flag has a value.
func (s *specSignature) setNullability(flag string, nullability sqlmodel.FunctionNullableBehaviour, hasValue bool) error {
	if hasValue {
		return fmt.Errorf("flag %q takes no value", flag)
	}
	if s.hasNullability {
		return fmt.Errorf("flag %q repeats the NULL behaviour", flag)
	}
	s.nullability = nullability
	s.hasNullability = true
	return nil
}

// validate checks that the positions the flags name fall within the argument list.
//
// Returns error when nulling-argument or return-type-from names a missing argument.
func (s *specSignature) validate() error {
	if s.nullability == sqlmodel.FunctionNullableNullWhenArgumentNull && s.nullingArgument >= len(s.arguments) {
		return fmt.Errorf("nulling-argument=%d names no argument", s.nullingArgument)
	}
	if s.returnTypeFromArgument > len(s.arguments) {
		return fmt.Errorf("return-type-from=%d names no argument", s.returnTypeFromArgument)
	}
	return nil
}

// specArgument is one decoded argument.
type specArgument struct {
	// name is the argument name, or empty.
	name string

	// typeSpelling is the spelling of the argument type, read while the line is decoded.
	typeSpelling string

	// typeIndex is the index of the argument type's spelling.
	typeIndex int

	// optional reports an argument with a default that may be left out.
	optional bool

	// notNull reports an argument that must not be NULL.
	notNull bool
}

// specScanner tracks bracket depth and single quotes while a spec line is scanned.
type specScanner struct {
	// depth is the number of parentheses and square brackets open.
	depth int

	// quoted reports a position inside single quotes.
	quoted bool
}

// step advances the scanner over one byte.
//
// Takes character (byte) which is the byte scanned.
//
// Returns bool which is true when the byte closes a bracket.
func (s *specScanner) step(character byte) bool {
	switch {
	case character == '\'':
		s.quoted = !s.quoted
	case s.quoted:
	case character == '(' || character == '[':
		s.depth++
	case character == ')' || character == ']':
		s.depth--
		return true
	}
	return false
}

// ParseFunctionSpec decodes a function spec.
//
// A line that does not decode is left out of the result and reported in the error, so the
// caller always gets every signature that did decode. An embedded spec is fixed at build
// time, so a dialect's tests assert that it decodes without error.
//
// Takes source (string) which is the spec text.
//
// Returns *FunctionSpec which holds every signature that decoded, in line order.
// Returns error which joins one error per line that did not decode, or nil.
func ParseFunctionSpec(source string) (*FunctionSpec, error) {
	spec := &FunctionSpec{
		spellings:  nil,
		signatures: make([]specSignature, 0, strings.Count(source, "\n")+1),
	}
	indexes := make(map[string]int)
	var problems []error
	lineNumber := 0
	for line := range strings.Lines(source) {
		lineNumber++
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}
		signature, returnType, err := parseSpecLine(trimmed)
		if err != nil {
			problems = append(problems, fmt.Errorf("function spec line %d: %w", lineNumber, err))
			continue
		}
		signature.returnType = spec.spellingIndex(indexes, returnType)
		for index := range signature.arguments {
			signature.arguments[index].typeIndex = spec.spellingIndex(indexes, signature.arguments[index].typeSpelling)
		}
		spec.signatures = append(spec.signatures, signature)
	}
	return spec, errors.Join(problems...)
}

// EmbeddedFunctionSpec returns a function that decodes a spec embedded in a dialect the
// first time it is called and gives the same result on every call after.
//
// The result holds every signature that decoded, as ParseFunctionSpec gives them, and an
// error naming each line that did not, so a dialect can register what decoded and still
// report the rest.
//
// Takes source (string) which is the spec text.
//
// Returns func() (*FunctionSpec, error) which returns the decoded spec, and an error
// wrapping the decode error when a line did not decode.
func EmbeddedFunctionSpec(source string) func() (*FunctionSpec, error) {
	return sync.OnceValues(func() (*FunctionSpec, error) {
		spec, err := ParseFunctionSpec(source)
		if err != nil {
			return spec, fmt.Errorf("decoding embedded function spec: %w", err)
		}
		return spec, nil
	})
}

// parseSpecLine decodes one signature line.
//
// Takes line (string) which is the trimmed line.
//
// Returns specSignature which is the decoded signature, its type indexes not yet set.
// Returns string which is the spelling of the result type.
// Returns error when the line is malformed.
func parseSpecLine(line string) (specSignature, string, error) {
	signature := specSignature{}
	open := strings.IndexByte(line, '(')
	if open <= 0 {
		return signature, "", errors.New("expected a function name followed by (")
	}
	signature.name = line[:open]
	if strings.ContainsAny(signature.name, " \t") {
		return signature, "", fmt.Errorf("function name %q holds a space", signature.name)
	}
	closing := closingBracket(line, open)
	if closing < 0 || !balancedBrackets(line) {
		return signature, "", errors.New("unbalanced brackets or quotes")
	}
	arguments, err := parseSpecArguments(line[open+1 : closing])
	if err != nil {
		return signature, "", err
	}
	signature.arguments = arguments
	words := topLevelFields(line[closing+1:], isSpecSpace, false)
	if len(words) == 0 {
		return signature, "", errors.New("missing return type")
	}
	for _, flag := range words[1:] {
		if err := signature.applyFlag(flag); err != nil {
			return signature, "", err
		}
	}
	err = signature.validate()
	return signature, words[0], err
}

// parseSpecArguments decodes a comma-separated argument list.
//
// Takes text (string) which is the text between the parentheses.
//
// Returns []specArgument which holds the arguments in order, or nil when there are none.
// Returns error when an argument is malformed.
func parseSpecArguments(text string) ([]specArgument, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	parts := topLevelFields(text, func(character byte) bool { return character == ',' }, true)
	arguments := make([]specArgument, 0, len(parts))
	for _, part := range parts {
		argument, err := parseSpecArgument(part)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)
	}
	return arguments, nil
}

// parseSpecArgument decodes an argument name, type and optional suffix markers.
//
// Takes text (string) which is the argument text.
//
// Returns specArgument which is the decoded argument.
// Returns error when the argument is empty or holds more than a name and a type.
func parseSpecArgument(text string) (specArgument, error) {
	argument := specArgument{}
	text = strings.TrimSpace(text)
	for text != "" {
		last := text[len(text)-1]
		if last == specMarkerOptional && !argument.optional {
			argument.optional = true
		} else if last == specMarkerNotNull && !argument.notNull {
			argument.notNull = true
		} else {
			break
		}
		text = strings.TrimSpace(text[:len(text)-1])
	}
	words := topLevelFields(text, isSpecSpace, false)
	switch len(words) {
	case 1:
		argument.typeSpelling = words[0]
	case 2:
		argument.name = words[0]
		argument.typeSpelling = words[1]
	default:
		return argument, fmt.Errorf("argument %q is not a type with an optional name", text)
	}
	return argument, nil
}

// specNullability maps a NULL behaviour flag to its behaviour.
//
// Takes flag (string) which is null-on-null, never-null or called-on-null.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the behaviour.
func specNullability(flag string) sqlmodel.FunctionNullableBehaviour {
	switch flag {
	case specFlagNullOnNull:
		return sqlmodel.FunctionNullableReturnsNullOnNull
	case specFlagNeverNull:
		return sqlmodel.FunctionNullableNeverNull
	default:
		return sqlmodel.FunctionNullableCalledOnNull
	}
}

// specCount parses a flag's non-negative whole-number value.
//
// Takes flag (string) which is the whole flag, for the error.
// Takes value (string) which is the text after =.
// Takes hasValue (bool) which is true when the flag has a value.
//
// Returns int which is the value.
// Returns error when the value is missing, not a whole number or negative.
func specCount(flag string, value string, hasValue bool) (int, error) {
	count, err := strconv.Atoi(value)
	if !hasValue || err != nil || count < 0 {
		return 0, fmt.Errorf("flag %q needs a non-negative whole number", flag)
	}
	return count, nil
}

// isSpecSpace reports whether a byte separates words in a spec line.
//
// Takes character (byte) which is the byte to test.
//
// Returns bool which is true for a space or a tab.
func isSpecSpace(character byte) bool {
	return character == ' ' || character == '\t'
}

// closingBracket finds the parenthesis that closes the one at open.
//
// Takes text (string) which is the text to search.
// Takes open (int) which is the offset of the opening parenthesis.
//
// Returns int which is the offset of the closing parenthesis, or -1 when it is missing.
func closingBracket(text string, open int) int {
	scanner := specScanner{}
	for index := open; index < len(text); index++ {
		if scanner.step(text[index]) && scanner.depth == 0 {
			return index
		}
	}
	return -1
}

// balancedBrackets reports whether every parenthesis and square bracket in text closes in
// order and every single quote is closed.
//
// Takes text (string) which is the text to check.
//
// Returns bool which is true when the brackets and quotes balance.
func balancedBrackets(text string) bool {
	scanner := specScanner{}
	for index := range len(text) {
		scanner.step(text[index])
		if scanner.depth < 0 {
			return false
		}
	}
	return scanner.depth == 0 && !scanner.quoted
}

// topLevelFields splits text at the separator bytes that lie outside parentheses, square
// brackets and single quotes, trimming each field.
//
// Takes text (string) which is the text to split.
// Takes separator (func(byte) bool) which reports a separating byte.
// Takes keepEmpty (bool) which keeps empty fields rather than dropping them, so a
// separator with nothing beside it shows.
//
// Returns []string which holds the fields in order.
func topLevelFields(text string, separator func(byte) bool, keepEmpty bool) []string {
	var fields []string
	scanner := specScanner{}
	start := 0
	for index := range len(text) {
		character := text[index]
		if scanner.depth == 0 && !scanner.quoted && separator(character) {
			fields = appendSpecField(fields, text[start:index], keepEmpty)
			start = index + 1
			continue
		}
		scanner.step(character)
	}
	return appendSpecField(fields, text[start:], keepEmpty)
}

// appendSpecField appends a trimmed field, unless it is empty and empty fields are
// dropped.
//
// Takes fields ([]string) which holds the fields so far.
// Takes field (string) which is the field to add.
// Takes keepEmpty (bool) which keeps an empty field.
//
// Returns []string which holds the fields with field added.
func appendSpecField(fields []string, field string, keepEmpty bool) []string {
	if trimmed := strings.TrimSpace(field); trimmed != "" || keepEmpty {
		return append(fields, trimmed)
	}
	return fields
}
