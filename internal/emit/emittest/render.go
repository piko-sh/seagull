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
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// directiveNamespace is the namespace of the directives the corpus writes, the
	// generator's default.
	directiveNamespace = "sgl."

	// parameterOpen opens a parameter reference in a corpus template.
	parameterOpen = "{"

	// parameterClose closes a parameter reference in a corpus template.
	parameterClose = "}"

	// identifierOpen opens a quoted identifier in a corpus template.
	identifierOpen = "[["

	// identifierClose closes a quoted identifier in a corpus template.
	identifierClose = "]]"
)

var (
	// errNoNamedForm reports a parameter written twice in a dialect that writes parameters
	// anonymously and has no named form to repeat it with.
	errNoNamedForm = errors.New("the dialect writes parameters anonymously and has no named form to write one twice")

	// errNoAnchor reports a parameter that needs a directive in a dialect with no form a
	// directive line can anchor it with.
	errNoAnchor = errors.New("the dialect has no numbered form for a directive line to anchor an anonymous placeholder with")
)

// dialectSyntax is what the suite reads from a dialect to write the corpus in its syntax.
type dialectSyntax struct {
	// body is the form query bodies write parameters in: the execution form when the dialect
	// reads it, and otherwise the first form it reads.
	body dialect.PlaceholderSigil

	// named is a named form, used for a parameter written twice where body is anonymous.
	named dialect.PlaceholderSigil

	// anchor is the numbered form a directive line anchors an anonymous placeholder with.
	anchor dialect.PlaceholderSigil

	// core holds the dialect's core types, which type the corpus tables and placeholders.
	core dialect.CoreTypes

	// info describes the dialect.
	info dialect.Info

	// hasNamed is true when named is set.
	hasNamed bool

	// hasAnchor is true when anchor is set.
	hasAnchor bool
}

// newDialectSyntax reads what the suite needs from a dialect.
//
// Takes engine (dialect.DialectPort) which is the dialect.
//
// Returns dialectSyntax which describes how to write the corpus for it.
func newDialectSyntax(engine dialect.DialectPort) dialectSyntax {
	info := engine.Info()
	syntax := dialectSyntax{info: info, core: engine.CoreTypes(), body: info.Placeholders.Execution,
		named: dialect.PlaceholderSigil{}, anchor: dialect.PlaceholderSigil{}, hasNamed: false, hasAnchor: false}
	if !slices.Contains(info.Placeholders.Sigils, info.Placeholders.Execution) && len(info.Placeholders.Sigils) > 0 {
		syntax.body = info.Placeholders.Sigils[0]
	}
	for _, sigil := range info.Placeholders.Sigils {
		switch {
		case sigil.Kind == dialect.PlaceholderNamed && !syntax.hasNamed:
			syntax.named, syntax.hasNamed = sigil, true
		case sigil.Kind == dialect.PlaceholderNumbered && !syntax.hasAnchor:
			syntax.anchor, syntax.hasAnchor = sigil, true
		}
	}
	return syntax
}

// typeOf returns the dialect type a corpus column of a kind takes: the dialect's core
// type, the enum the case's schema declares, or a nested shape built of them.
//
// Takes kind (columnKind) which is the kind.
//
// Returns sqlmodel.SQLType which is the dialect's type.
func (syntax *dialectSyntax) typeOf(kind columnKind) sqlmodel.SQLType {
	switch kind {
	case columnText, columnOverriddenText:
		return syntax.core.Text
	case columnJSON:
		return syntax.core.JSON
	case columnEnum:
		return syntax.enumType()
	case columnNullableEnums:
		element := syntax.enumType()
		element.Nullable = true
		return arrayOf(element)
	case columnEnumMap:
		return mapOf(syntax.core.Text, syntax.enumType())
	case columnEnumGrid:
		return arrayOf(arrayOf(syntax.enumType()))
	default:
		return syntax.core.Count
	}
}

// enumType returns the enum the case's schema declares, named as the dialect folds it in
// the corpus's schema.
//
// Returns sqlmodel.SQLType which is the enum.
func (syntax *dialectSyntax) enumType() sqlmodel.SQLType {
	enum := sqlmodel.SQLType{}
	enum.Category = sqlmodel.TypeCategoryEnum
	enum.EngineName = syntax.info.Identifiers.Fold(enumName, false)
	enum.Schema = syntax.schemaName()
	enum.EnumValues = enumValueList
	return enum
}

// placeholderType returns the type a typed placeholder of a parameter of a kind carries:
// the dialect's general text type for a kind holding the enum, which names no type of its
// own in a placeholder, and the column's type otherwise.
//
// Takes kind (columnKind) which is the kind.
//
// Returns sqlmodel.SQLType which is the type.
func (syntax *dialectSyntax) placeholderType(kind columnKind) sqlmodel.SQLType {
	if holdsEnum(kind) {
		return syntax.core.Text
	}
	return syntax.typeOf(kind)
}

// schemaName returns the schema the corpus's tables and enum are declared in: the first
// of the dialect's search path, folded as the dialect folds identifiers, or none.
//
// Returns string which is the schema's name.
func (syntax *dialectSyntax) schemaName() string {
	if len(syntax.info.SchemaSearchPath) == 0 {
		return ""
	}
	return syntax.info.Identifiers.Fold(syntax.info.SchemaSearchPath[0], false)
}

// renderCase writes a case's query files in the dialect's syntax.
//
// Takes current (corpusCase) which is the case.
// Takes variants (map[string]int) which picks the variant of each query by name; a query
// it does not list takes its first, and one it maps to droppedVariant is left out, as is
// a file left with no query.
// Takes reversed (bool) which writes each file's queries in reverse order.
//
// Returns map[string]string which maps each query file's name to its content.
// Returns error when a query cannot be written in the dialect.
func (syntax *dialectSyntax) renderCase(current corpusCase, variants map[string]int, reversed bool) (map[string]string, error) {
	files := make(map[string]string, len(current.files))
	for _, file := range current.files {
		queries := slices.Clone(file.queries)
		if reversed {
			slices.Reverse(queries)
		}
		rendered := make([]string, 0, len(queries))
		for index := range queries {
			entry := &queries[index]
			if variants[entry.name] == droppedVariant {
				continue
			}
			text, err := syntax.renderQuery(entry, variants[entry.name])
			if err != nil {
				return nil, fmt.Errorf("writing %s: %w", entry.name, err)
			}
			rendered = append(rendered, text)
		}
		if len(rendered) > 0 {
			files[file.name] = strings.Join(rendered, "\n")
		}
	}
	return files, nil
}

// renderQuery writes one query, its directives and its SQL, in the dialect's syntax.
//
// Takes entry (*corpusQuery) which is the query.
// Takes variant (int) which picks the SQL variant.
//
// Returns string which is the query's text, ending in a newline.
// Returns error when the query cannot be written in the dialect.
func (syntax *dialectSyntax) renderQuery(entry *corpusQuery, variant int) (string, error) {
	template := entry.variants[variant]
	order := parameterOrder(template, entry.parameters)
	mode, err := syntax.modeFor(order)
	if err != nil {
		return "", err
	}
	prefix := syntax.info.Comments.LinePrefix + " "
	var builder strings.Builder
	for line := range strings.SplitSeq(entry.description, "\n") {
		if entry.description != "" {
			builder.WriteString(strings.TrimRight(prefix+line, " ") + "\n")
		}
	}
	builder.WriteString(prefix + directiveNamespace + "query(name: " + entry.name + ", command: " + entry.command)
	if entry.options != "" {
		builder.WriteString(", " + entry.options)
	}
	builder.WriteString(")\n")
	for _, header := range entry.headers {
		builder.WriteString(prefix + directiveNamespace + header + "\n")
	}
	directives, err := syntax.parameterDirectives(entry.parameters, order, mode)
	if err != nil {
		return "", err
	}
	for _, directive := range directives {
		builder.WriteString(prefix + directive + "\n")
	}
	builder.WriteString(syntax.substitute(template, entry.parameters, order, mode))
	builder.WriteString("\n")
	return builder.String(), nil
}

// modeFor chooses how a query writes its parameters: in the body form, or in a named form
// when the body form is anonymous and a parameter is written twice.
//
// Takes order ([]string) which lists the parameter references in the order written.
//
// Returns placeholderMode which is the chosen form.
// Returns error when a parameter written twice cannot be.
func (syntax *dialectSyntax) modeFor(order []string) (placeholderMode, error) {
	mode := placeholderMode{sigil: syntax.body, anchor: syntax.body, hasAnchor: true}
	if syntax.body.Kind != dialect.PlaceholderAnonymous {
		return mode, nil
	}
	if repeats(order) {
		if !syntax.hasNamed {
			return mode, errNoNamedForm
		}
		return placeholderMode{sigil: syntax.named, anchor: syntax.named, hasAnchor: true}, nil
	}
	mode.anchor, mode.hasAnchor = syntax.anchor, syntax.hasAnchor
	return mode, nil
}

// parameterDirectives writes the directive line of each parameter that needs one: every
// parameter of a positional form, which a directive names, and a parameter of a named
// form with a quality to declare.
//
// Takes parameters ([]corpusParameter) which declares the parameters.
// Takes order ([]string) which lists the parameter references in the order written.
// Takes mode (placeholderMode) which is the query's form.
//
// Returns []string which holds the directive lines without their comment prefix.
// Returns error when a parameter needs a directive no form can anchor.
func (syntax *dialectSyntax) parameterDirectives(parameters []corpusParameter, order []string, mode placeholderMode) ([]string, error) {
	var directives []string
	for _, parameter := range parameters {
		named := mode.sigil.Kind == dialect.PlaceholderNamed
		qualities := qualitiesOf(parameter)
		if named && qualities == "" {
			continue
		}
		if !mode.hasAnchor {
			return nil, errNoAnchor
		}
		position := slices.Index(order, parameter.name)
		if position < 0 {
			continue
		}
		anchor := mode.anchor.Render(numberOf(parameter.name, order), parameter.name, syntax.placeholderType(parameter.kind).EngineName)
		directives = append(directives, anchor+" as "+directiveNamespace+"param("+parameter.name+qualities+")")
	}
	return directives, nil
}

// substitute writes a template's SQL: each parameter reference becomes its placeholder
// and each quoted identifier takes the dialect's quotes.
//
// Takes template (string) which is the query's template.
// Takes parameters ([]corpusParameter) which declares the parameters.
// Takes order ([]string) which lists the parameter references in the order written.
// Takes mode (placeholderMode) which is the query's form.
//
// Returns string which is the SQL.
func (syntax *dialectSyntax) substitute(template string, parameters []corpusParameter, order []string, mode placeholderMode) string {
	var builder strings.Builder
	occurrence := 0
	for position := 0; position < len(template); {
		if name, width, ok := parameterAt(template, position, parameters); ok {
			occurrence++
			number := numberOf(name, order)
			if mode.sigil.Kind == dialect.PlaceholderAnonymous {
				number = occurrence
			}
			kind := parameters[slices.IndexFunc(parameters, func(parameter corpusParameter) bool { return parameter.name == name })].kind
			builder.WriteString(mode.sigil.Render(number, name, syntax.placeholderType(kind).EngineName))
			position += width
			continue
		}
		if name, width, ok := identifierAt(template, position); ok {
			builder.WriteString(syntax.info.IdentifierQuote.Open + name + syntax.info.IdentifierQuote.Close)
			position += width
			continue
		}
		builder.WriteByte(template[position])
		position++
	}
	return builder.String()
}

// catalogue builds the catalogue of a case's tables, typed with the dialect's core types
// and named as the dialect folds identifiers, in its default schema.
//
// Takes tables ([]corpusTable) which are the case's tables.
//
// Returns *sqlmodel.Catalogue which holds the tables.
func (syntax *dialectSyntax) catalogue(tables []corpusTable) *sqlmodel.Catalogue {
	identifiers := syntax.info.Identifiers
	searchPath := make([]string, 0, len(syntax.info.SchemaSearchPath))
	for _, entry := range syntax.info.SchemaSearchPath {
		searchPath = append(searchPath, identifiers.Fold(entry, false))
	}
	schema := &sqlmodel.Schema{}
	if len(searchPath) > 0 {
		schema.Name = searchPath[0]
	}
	schema.Tables = make(map[string]*sqlmodel.Table, len(tables))
	schema.Views = map[string]*sqlmodel.View{}
	schema.Enums = map[string]*sqlmodel.Enum{}
	schema.Functions = map[string][]*sqlmodel.Function{}
	schema.CompositeTypes = map[string]*sqlmodel.CompositeType{}
	schema.Sequences = map[string]*sqlmodel.Sequence{}
	schema.Domains = map[string]*sqlmodel.Domain{}
	for _, table := range tables {
		built := syntax.table(table, schema.Name)
		schema.Tables[identifiers.Key(built.Name)] = built
		if slices.ContainsFunc(table.columns, func(column corpusColumn) bool { return holdsEnum(column.kind) }) {
			enum := syntax.enumType()
			schema.Enums[identifiers.Key(enum.EngineName)] = &sqlmodel.Enum{Name: enum.EngineName, Schema: schema.Name, Values: enum.EnumValues,
				Comment: "", Origin: sqlmodel.MigrationOrigin{}}
		}
	}
	catalogue := &sqlmodel.Catalogue{}
	catalogue.Schemas = map[string]*sqlmodel.Schema{identifiers.Key(schema.Name): schema}
	catalogue.Extensions = map[string]struct{}{}
	catalogue.DefaultSchema = schema.Name
	catalogue.SearchPath = searchPath
	catalogue.Identifiers = identifiers
	return catalogue
}

// table builds one catalogue table, keyed on its first column.
//
// Takes table (corpusTable) which is the corpus table.
// Takes schemaName (string) which is the schema the table belongs to.
//
// Returns *sqlmodel.Table which is the table.
func (syntax *dialectSyntax) table(table corpusTable, schemaName string) *sqlmodel.Table {
	identifiers := syntax.info.Identifiers
	built := &sqlmodel.Table{}
	built.Name = table.name
	if !table.quoted {
		built.Name = identifiers.Fold(table.name, false)
	}
	built.Schema = schemaName
	for _, column := range table.columns {
		entry := sqlmodel.Column{}
		entry.Name = identifiers.Fold(column.name, false)
		entry.SQLType = syntax.typeOf(column.kind)
		entry.Nullable = column.nullable
		entry.GoTypeOverride = overrideOf(column.kind)
		built.Columns = append(built.Columns, entry)
	}
	if len(built.Columns) > 0 {
		built.PrimaryKey = []string{built.Columns[0].Name}
	}
	return built
}

// placeholderMode says how one query writes its parameters.
type placeholderMode struct {
	// sigil is the form the query's placeholders take.
	sigil dialect.PlaceholderSigil

	// anchor is the form its directive lines anchor parameters with.
	anchor dialect.PlaceholderSigil

	// hasAnchor is false when no directive line can anchor its parameters.
	hasAnchor bool
}

// corpusSchema is the schema source that hands the generator a corpus case's tables.
type corpusSchema struct {
	// catalogue is the case's catalogue.
	catalogue *sqlmodel.Catalogue
}

// BuildCatalogue hands the generator the case's catalogue.
//
// Returns *sqlmodel.Catalogue which is the catalogue.
// Returns []diag.SourceError which is always empty.
// Returns error which is always nil.
func (schema *corpusSchema) BuildCatalogue(context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	return schema.catalogue, nil, nil
}

// arrayOf returns an array of an element type.
//
// Takes element (sqlmodel.SQLType) which is the element's type.
//
// Returns sqlmodel.SQLType which is the array.
func arrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	array := sqlmodel.SQLType{}
	array.Category = sqlmodel.TypeCategoryArray
	array.ElementType = &element
	return array
}

// mapOf returns a map from a key type to a value type.
//
// Takes key (sqlmodel.SQLType) which is the key's type.
// Takes value (sqlmodel.SQLType) which is the value's type.
//
// Returns sqlmodel.SQLType which is the map.
func mapOf(key, value sqlmodel.SQLType) sqlmodel.SQLType {
	mapped := sqlmodel.SQLType{}
	mapped.Category = sqlmodel.TypeCategoryMap
	mapped.KeyType = &key
	mapped.ElementType = &value
	return mapped
}

// parameterOrder lists the parameter references of a template in the order written, one
// entry per reference.
//
// Takes template (string) which is the query's template.
// Takes parameters ([]corpusParameter) which declares the parameters.
//
// Returns []string which holds the referenced names.
func parameterOrder(template string, parameters []corpusParameter) []string {
	var order []string
	for position := 0; position < len(template); position++ {
		if name, width, ok := parameterAt(template, position, parameters); ok {
			order = append(order, name)
			position += width - 1
		}
	}
	return order
}

// parameterAt reads a parameter reference at a position of a template: an open brace, the
// name of a declared parameter and a close brace.
//
// Takes template (string) which is the query's template.
// Takes position (int) which is the byte offset to read at.
// Takes parameters ([]corpusParameter) which declares the parameters.
//
// Returns string which is the parameter's name.
// Returns int which is the reference's width in bytes.
// Returns bool which is false when no reference starts there.
func parameterAt(template string, position int, parameters []corpusParameter) (string, int, bool) {
	if !strings.HasPrefix(template[position:], parameterOpen) {
		return "", 0, false
	}
	name, _, found := strings.Cut(template[position+len(parameterOpen):], parameterClose)
	if !found || !slices.ContainsFunc(parameters, func(parameter corpusParameter) bool { return parameter.name == name }) {
		return "", 0, false
	}
	return name, len(parameterOpen) + len(name) + len(parameterClose), true
}

// identifierAt reads a quoted identifier at a position of a template.
//
// Takes template (string) which is the query's template.
// Takes position (int) which is the byte offset to read at.
//
// Returns string which is the identifier.
// Returns int which is the reference's width in bytes.
// Returns bool which is false when no quoted identifier starts there.
func identifierAt(template string, position int) (string, int, bool) {
	if !strings.HasPrefix(template[position:], identifierOpen) {
		return "", 0, false
	}
	name, _, found := strings.Cut(template[position+len(identifierOpen):], identifierClose)
	if !found {
		return "", 0, false
	}
	return name, len(identifierOpen) + len(name) + len(identifierClose), true
}

// numberOf returns a parameter's number: its position among the distinct parameters in
// the order first written, counting from one.
//
// Takes name (string) which is the parameter's name.
// Takes order ([]string) which lists the parameter references in the order written.
//
// Returns int which is the number, or zero for a parameter never written.
func numberOf(name string, order []string) int {
	return slices.Index(firstAppearances(order), name) + 1
}

// firstAppearances lists each referenced parameter once, in the order first written.
//
// Takes order ([]string) which lists the parameter references in the order written.
//
// Returns []string which holds each name once.
func firstAppearances(order []string) []string {
	seen := make(map[string]bool, len(order))
	distinct := make([]string, 0, len(order))
	for _, name := range order {
		if !seen[name] {
			seen[name] = true
			distinct = append(distinct, name)
		}
	}
	return distinct
}

// repeats reports whether any parameter is referenced more than once.
//
// Takes order ([]string) which lists the parameter references in the order written.
//
// Returns bool which is true when a name appears twice.
func repeats(order []string) bool {
	return len(firstAppearances(order)) != len(order)
}

// qualitiesOf writes the keyword arguments a parameter's directive declares.
//
// Takes parameter (corpusParameter) which is the parameter.
//
// Returns string which holds each quality after a comma, or nothing.
func qualitiesOf(parameter corpusParameter) string {
	var qualities string
	if parameter.optional {
		qualities += ", optional: true"
	}
	if parameter.slice {
		qualities += ", kind: slice"
	}
	return qualities
}

// capitalised returns a name with its first letter in upper case.
//
// Takes name (string) which is the name.
//
// Returns string which is the capitalised name.
func capitalised(name string) string {
	if name == "" {
		return name
	}
	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// numberedName names a value by its position, such as for a generated package.
//
// Takes prefix (string) which starts the name.
// Takes index (int) which is the position.
//
// Returns string which is the name.
func numberedName(prefix string, index int) string {
	return prefix + strconv.Itoa(index)
}
