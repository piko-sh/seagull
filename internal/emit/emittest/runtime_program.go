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
	"go/types"
	"slices"
	"strconv"
	"strings"
)

const (
	// labelPlain is the call of a query with every argument set.
	labelPlain = "plain"

	// labelNil is the call with every non-byte slice nil.
	labelNil = "nil"

	// labelEmpty is the call with every non-byte slice empty.
	labelEmpty = "empty"

	// labelOne is the call with every non-byte slice of one element.
	labelOne = "one"

	// labelMany is the call with every non-byte slice of three elements.
	labelMany = "many"

	// labelBatch is the call of a batch with three items.
	labelBatch = "batch"

	// labelSorted is the call of a sortable query sorted by a column it allows.
	labelSorted = "sorted"

	// labelAll is the run of a builder's All with a filter, an order and pagination.
	labelAll = "all"

	// labelCount is the run of a builder's Count with a filter.
	labelCount = "count"

	// manyElements is how many elements a many slice, and a batch, holds.
	manyElements = 3

	// nilLength asks the sampler for a nil slice.
	nilLength = -1

	// sampleNumberBase starts the integers the sampler writes.
	sampleNumberBase = 7000

	// smallNumberBase starts the integers the sampler writes for an eight-bit type.
	smallNumberBase = 10

	// smallNumberRange bounds the integers the sampler writes for an eight-bit type.
	smallNumberRange = 100

	// builderLimit is the row limit a builder run asks for.
	builderLimit = 5

	// builderOffset is the offset a builder run asks for.
	builderOffset = 10

	// builderFilterColumn is the column a builder run filters on.
	builderFilterColumn = columnKindName

	// builderOrderColumn is the column a builder run orders by.
	builderOrderColumn = columnID

	// timePackage is the import path of the time package.
	timePackage = "time"

	// errorTypeName is the name of the error type.
	errorTypeName = "error"

	// secondsInMinute bounds the seconds of a sampled time.
	secondsInMinute = 60

	// conversionOpen opens a conversion or call in the program's source.
	conversionOpen = "("

	// conversionClose closes a conversion or call in the program's source.
	conversionClose = ")"

	// nilSliceRecord starts the statement recording whether a call's rows came back nil.
	nilSliceRecord = "emittestNilSlice = "
)

// expectedValue is a value the runtime lane passed to a method, which must reach the
// driver.
type expectedValue struct {
	// path says which argument or field the value was passed in.
	path string

	// text is the value as the program writes it, which must appear in an argument or the
	// SQL the driver receives.
	text string
}

// sampler writes Go expressions for the values the runtime lane passes and records the
// ones that must reach the driver. Strings and numbers vary to expose missing arguments.
type sampler struct {
	// generated is the generated package.
	generated *types.Package

	// qualifier names packages as the program imports them.
	qualifier types.Qualifier

	// importAlias returns the alias the program imports a path under.
	importAlias func(importPath string) string

	// counter numbers the values, shared by every call of a package.
	counter *int

	// expected gathers the values that must reach the driver.
	expected []expectedValue

	// length is how many elements a slice holds, or nilLength for nil.
	length int
}

// value writes a value of a type.
//
// Takes candidate (types.Type) which is the type.
// Takes path (string) which names where the value goes, for messages.
//
// Returns string which is the Go expression.
func (current *sampler) value(candidate types.Type, path string) string {
	switch shape := types.Unalias(candidate).(type) {
	case *types.Basic:
		return current.basic(shape, shape.Name(), path)
	case *types.Pointer:
		return "new" + conversionOpen + current.value(shape.Elem(), path) + conversionClose
	case *types.Slice:
		return current.slice(shape, types.TypeString(shape, current.qualifier), path)
	case *types.Named:
		return current.named(shape, path)
	case *types.Interface:
		return current.text(path)
	default:
		return zeroValue(types.TypeString(candidate, current.qualifier))
	}
}

// basic writes a value of a basic type, or of a named type over one.
//
// Takes basic (*types.Basic) which is the basic type.
// Takes typeName (string) which is the type to convert to.
// Takes path (string) which names where the value goes.
//
// Returns string which is the Go expression.
func (current *sampler) basic(basic *types.Basic, typeName, path string) string {
	properties := basic.Info()
	switch {
	case properties&types.IsString != 0:
		return typeName + conversionOpen + current.text(path) + conversionClose
	case properties&types.IsBoolean != 0:
		return typeName + conversionOpen + "true" + conversionClose
	case properties&types.IsInteger != 0:
		number := current.next() + sampleNumberBase
		if basic.Kind() == types.Int8 || basic.Kind() == types.Uint8 {
			number = smallNumberBase + number%smallNumberRange
		}
		current.expect(path, strconv.Itoa(number))
		return typeName + conversionOpen + strconv.Itoa(number) + conversionClose
	case properties&types.IsFloat != 0:
		number := strconv.Itoa(current.next()+sampleNumberBase) + ".5"
		current.expect(path, number)
		return typeName + conversionOpen + number + conversionClose
	default:
		return zeroValue(typeName)
	}
}

// text writes a distinct string literal.
//
// Takes path (string) which names where the value goes.
//
// Returns string which is the quoted literal.
func (current *sampler) text(path string) string {
	value := fmt.Sprintf("emittest%04d", current.next())
	current.expect(path, value)
	return strconv.Quote(value)
}

// slice writes a byte slice from a sampled string, or another slice holding as many
// elements as the call asks for.
//
// Takes shape (*types.Slice) which is the slice type.
// Takes typeName (string) which is the type to write.
// Takes path (string) which names where the value goes.
//
// Returns string which is the Go expression.
func (current *sampler) slice(shape *types.Slice, typeName, path string) string {
	if basic, ok := types.Unalias(shape.Elem()).(*types.Basic); ok && basic.Kind() == types.Byte {
		return typeName + conversionOpen + current.text(path) + conversionClose
	}
	switch current.length {
	case nilLength:
		return typeName + conversionOpen + "nil" + conversionClose
	case 0:
		return typeName + "{}"
	}
	elements := make([]string, 0, current.length)
	for index := range current.length {
		elements = append(elements, current.value(shape.Elem(), fmt.Sprintf("%s[%d]", path, index)))
	}
	return typeName + "{" + strings.Join(elements, ", ") + "}"
}

// named writes a value of a named type: a time, the first constant the generated package
// declares of the type, or a value built from the type's structure.
//
// Takes named (*types.Named) which is the type.
// Takes path (string) which names where the value goes.
//
// Returns string which is the Go expression.
func (current *sampler) named(named *types.Named, path string) string {
	typeName := types.TypeString(named, current.qualifier)
	object := named.Obj()
	if object.Pkg() != nil && object.Pkg().Path() == timePackage && object.Name() == "Time" {
		return current.time(path)
	}
	if constant := current.firstConstant(named); constant != "" {
		return constant
	}
	switch underlying := named.Underlying().(type) {
	case *types.Basic:
		return current.basic(underlying, typeName, path)
	case *types.Slice:
		return current.slice(underlying, typeName, path)
	case *types.Struct:
		return current.structure(underlying, typeName, path)
	default:
		return zeroValue(typeName)
	}
}

// time writes a sampled time, cycling through the seconds of a minute.
//
// Takes path (string) which names where the value goes.
//
// Returns string which is the Go expression.
func (current *sampler) time(path string) string {
	second := current.next() % secondsInMinute
	current.expect(path, fmt.Sprintf("2001-02-03 04:05:%02d", second))
	alias := current.importAlias(timePackage)
	return fmt.Sprintf("%s.Date(2001, %s.February, 3, 4, 5, %d, 0, %s.UTC)", alias, alias, second, alias)
}

// firstConstant returns the first constant, by name, the generated package declares of a
// type, such as a column a query may be sorted by.
//
// Takes named (*types.Named) which is the type.
//
// Returns string which is the constant's qualified name, or nothing.
func (current *sampler) firstConstant(named *types.Named) string {
	if named.Obj().Pkg() != current.generated {
		return ""
	}
	scope := current.generated.Scope()
	for _, name := range scope.Names() {
		if constant, ok := scope.Lookup(name).(*types.Const); ok && types.Identical(constant.Type(), named) {
			return current.qualifier(current.generated) + "." + name
		}
	}
	return ""
}

// structure writes a struct with every exported field set.
//
// Takes structure (*types.Struct) which is the struct type.
// Takes typeName (string) which is the type to write.
// Takes path (string) which names where the value goes.
//
// Returns string which is the Go expression.
func (current *sampler) structure(structure *types.Struct, typeName, path string) string {
	var fields []string
	for field := range structure.Fields() {
		if field.Exported() {
			fields = append(fields, field.Name()+": "+current.value(field.Type(), path+"."+field.Name()))
		}
	}
	return typeName + "{" + strings.Join(fields, ", ") + "}"
}

// next returns the next number of the package's values.
//
// Returns int which is the number.
func (current *sampler) next() int {
	*current.counter++
	return *current.counter
}

// expect records a value that must reach the driver.
//
// Takes path (string) which names where the value went.
// Takes text (string) which is the value as written.
func (current *sampler) expect(path, text string) {
	current.expected = append(current.expected, expectedValue{path: path, text: text})
}

// labelsOf returns the calls the runtime lane makes of a query.
//
// Takes calls (scenario) which is the query's scenario.
//
// Returns []string which holds each call's label.
func labelsOf(calls scenario) []string {
	switch calls {
	case scenarioPlain:
		return []string{labelPlain}
	case scenarioSlices:
		return []string{labelNil, labelEmpty, labelOne, labelMany}
	case scenarioBatch:
		return []string{labelBatch}
	case scenarioSortable:
		return []string{labelSorted}
	case scenarioBuilder:
		return []string{labelAll, labelCount}
	case scenarioNone:
	}
	return nil
}

// sliceLength returns how many elements a call's slices hold, or nilLength for nil.
//
// Takes label (string) which is the call's label.
//
// Returns int which is the length.
func sliceLength(label string) int {
	switch label {
	case labelNil:
		return nilLength
	case labelEmpty:
		return 0
	case labelMany, labelBatch:
		return manyElements
	default:
		return 1
	}
}

// zeroValue writes the zero value of a type.
//
// Takes typeName (string) which is the type.
//
// Returns string which is the Go expression.
func zeroValue(typeName string) string {
	return "*new" + conversionOpen + typeName + conversionClose
}

// callSource writes the body of the function that calls a method once: it passes ctx when
// its first parameter is a context, and a value for each other argument, and returns the
// error the call reports.
//
// Takes method (*types.Func) which is the method.
// Takes values ([]string) which are the arguments after any context.
// Takes label (string) which is the call's label.
// Takes current (*sampler) which writes the builder's filter value.
// Takes fold (func(string) string) which spells a column as the dialect folds it.
//
// Returns string which is the function body.
func callSource(method *types.Func, values []string, label string, current *sampler, fold func(string) string) string {
	arguments := values
	if takesContext(method) {
		arguments = append([]string{"ctx"}, values...)
	}
	call := "queries." + method.Name() + conversionOpen + strings.Join(arguments, ", ") + conversionClose
	results := method.Signature().Results()
	switch {
	case results.Len() == 0:
		return call + "\nreturn nil"
	case results.Len() == 1 && iteratorYieldArity(results.At(0).Type()) == 2:
		return "for _, iterationError := range " + call + " {\nif iterationError != nil {\nreturn iterationError\n}\n}\nreturn nil"
	case results.Len() == 1 && iteratorYieldArity(results.At(0).Type()) == 1:
		return "for range " + call + " {\n}\nreturn nil"
	case results.Len() == 1 && isBuilder(results.At(0).Type()):
		return builderSource(call, results.At(0).Type(), label, current, fold)
	case results.Len() == 2 && isSlice(results.At(0).Type()) && isError(results.At(1).Type()):
		return "rows, err := " + call + "\n" + nilSliceRecord + "rows == nil\nreturn err"
	case isError(results.At(results.Len() - 1).Type()):
		return strings.Repeat("_, ", results.Len()-1) + "err := " + call + "\nreturn err"
	default:
		return "emittestDiscard(" + call + ")\nreturn nil"
	}
}

// builderSource writes the run of a runtime query builder: a filter, and for All an
// order, a limit and an offset, each through the builder's method of that name when it
// has one.
//
// Takes call (string) which creates the builder.
// Takes builder (types.Type) which is the builder's type.
// Takes label (string) which is labelAll or labelCount.
// Takes current (*sampler) which writes the filter's value.
// Takes fold (func(string) string) which spells a column as the dialect folds it.
//
// Returns string which is the function body.
func builderSource(call string, builder types.Type, label string, current *sampler, fold func(string) string) string {
	methods := methodNames(builder)
	folded := func(column string) string { return strconv.Quote(fold(column)) }
	lines := []string{"builder := " + call}
	if slices.Contains(methods, "Where") {
		lines = append(lines, "builder = builder.Where("+folded(builderFilterColumn)+", \"=\", "+current.text("Where value")+")")
	}
	terminal := "Count"
	if label == labelAll {
		terminal = "All"
		if slices.Contains(methods, "OrderBy") {
			lines = append(lines, "builder = builder.OrderBy("+folded(builderOrderColumn)+", \"DESC\")")
		}
		if slices.Contains(methods, "Limit") {
			lines = append(lines, "builder = builder.Limit("+strconv.Itoa(builderLimit)+")")
		}
		if slices.Contains(methods, "Offset") {
			lines = append(lines, "builder = builder.Offset("+strconv.Itoa(builderOffset)+")")
		}
	}
	if terminal == "All" {
		lines = append(lines, "rows, err := builder.All(ctx)", nilSliceRecord+"rows == nil", "return err")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "_, err := builder."+terminal+"(ctx)", "return err")
	return strings.Join(lines, "\n")
}

// returnsRows reports whether a call returns a slice of rows: a method returning a slice
// and an error, or a runtime builder's All.
//
// Takes method (*types.Func) which is the method called.
// Takes label (string) which is the call's label.
//
// Returns bool which is true when the call returns rows in a slice.
func returnsRows(method *types.Func, label string) bool {
	results := method.Signature().Results()
	if results.Len() == 1 && isBuilder(results.At(0).Type()) {
		return label == labelAll
	}
	return results.Len() == 2 && isSlice(results.At(0).Type()) && isError(results.At(1).Type())
}

// isSlice reports whether a type is a slice.
//
// Takes candidate (types.Type) which is the type.
//
// Returns bool which is true for a slice.
func isSlice(candidate types.Type) bool {
	_, ok := types.Unalias(candidate).Underlying().(*types.Slice)
	return ok
}

// iteratorYieldArity returns how many values an iterator function yields, or zero for a
// type that is not one.
//
// Takes candidate (types.Type) which is the type.
//
// Returns int which is the number of values each step yields.
func iteratorYieldArity(candidate types.Type) int {
	signature, ok := types.Unalias(candidate).Underlying().(*types.Signature)
	if !ok || signature.Params().Len() != 1 || signature.Results().Len() != 0 {
		return 0
	}
	yield, ok := types.Unalias(signature.Params().At(0).Type()).Underlying().(*types.Signature)
	if !ok {
		return 0
	}
	return yield.Params().Len()
}

// isBuilder reports whether a type is a runtime query builder: a pointer to a named type
// whose methods include All or Count.
//
// Takes candidate (types.Type) which is the type.
//
// Returns bool which is true for a builder.
func isBuilder(candidate types.Type) bool {
	methods := methodNames(candidate)
	return slices.Contains(methods, "All") || slices.Contains(methods, "Count")
}

// methodNames lists the methods of a type's method set.
//
// Takes candidate (types.Type) which is the type.
//
// Returns []string which holds the names.
func methodNames(candidate types.Type) []string {
	set := types.NewMethodSet(candidate)
	names := make([]string, 0, set.Len())
	for selection := range set.Methods() {
		names = append(names, selection.Obj().Name())
	}
	return names
}

// takesContext reports whether a method's first parameter is a context.Context, which a
// runtime builder's entry point does not take.
//
// Takes method (*types.Func) which is the method.
//
// Returns bool which is true when its first parameter is a context.
func takesContext(method *types.Func) bool {
	parameters := method.Signature().Params()
	return parameters.Len() > 0 && isContext(parameters.At(0).Type())
}

// isError reports whether a type is the error interface.
//
// Takes candidate (types.Type) which is the type.
//
// Returns bool which is true for error.
func isError(candidate types.Type) bool {
	named, ok := types.Unalias(candidate).(*types.Named)
	return ok && named.Obj().Pkg() == nil && named.Obj().Name() == errorTypeName
}
