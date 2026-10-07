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
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// typeKindEnum is the pg_type.typtype code of an enum type.
	typeKindEnum = "e"

	// typeKindComposite is the pg_type.typtype code of a composite type.
	typeKindComposite = "c"

	// typeKindDomain is the pg_type.typtype code of a domain.
	typeKindDomain = "d"

	// maxArrayDimensions caps the recorded depth of an array column. PostgreSQL itself
	// allows at most six dimensions, matching what migration replay records.
	maxArrayDimensions = 6

	// maxDomainDepth caps how many domains-over-domains are followed to reach a base type,
	// so a malformed catalogue cannot loop.
	maxDomainDepth = 16

	// attributeTypeColumns selects the type description of the pg_attribute row aliased a,
	// in the order typeDescription.scanDestinations expects. Arrays are described by their
	// element type and dimension count; pg_attribute.attndims is unreliable, so any array
	// counts as at least one dimension.
	attributeTypeColumns = `
			base_type.typname,
			base_namespace.nspname,
			base_type.typtype::text,
			(CASE WHEN element_type.oid IS NULL THEN 0 ELSE GREATEST(a.attndims, 1) END)::integer,
			pg_catalog.format_type(a.atttypid, a.atttypmod)`

	// attributeTypeJoins joins the type tables attributeTypeColumns reads. A true array type
	// is a variable-length type with an element type; fixed-length types such as name and
	// point also carry an element type but are not arrays.
	attributeTypeJoins = `
		 JOIN pg_catalog.pg_type attribute_type ON attribute_type.oid = a.atttypid
		 LEFT JOIN pg_catalog.pg_type element_type
			ON element_type.oid = attribute_type.typelem AND attribute_type.typlen = -1
		 JOIN pg_catalog.pg_type base_type ON base_type.oid = COALESCE(element_type.oid, attribute_type.oid)
		 JOIN pg_catalog.pg_namespace base_namespace ON base_namespace.oid = base_type.typnamespace`

	// domainsQuery lists every domain with the type it is declared over, described like an
	// attribute type.
	domainsQuery = `SELECT
			domain_namespace.nspname,
			domain_type.typname,
			domain_type.typnotnull,
			base_type.typname,
			base_namespace.nspname,
			base_type.typtype::text,
			(CASE WHEN element_type.oid IS NULL THEN 0 ELSE GREATEST(domain_type.typndims, 1) END)::integer,
			pg_catalog.format_type(domain_type.typbasetype, domain_type.typtypmod)
		 FROM pg_catalog.pg_type domain_type
		 JOIN pg_catalog.pg_namespace domain_namespace ON domain_namespace.oid = domain_type.typnamespace
		 JOIN pg_catalog.pg_type underlying_type ON underlying_type.oid = domain_type.typbasetype
		 LEFT JOIN pg_catalog.pg_type element_type
			ON element_type.oid = underlying_type.typelem AND underlying_type.typlen = -1
		 JOIN pg_catalog.pg_type base_type ON base_type.oid = COALESCE(element_type.oid, underlying_type.oid)
		 JOIN pg_catalog.pg_namespace base_namespace ON base_namespace.oid = base_type.typnamespace
		 WHERE domain_type.typtype = 'd'
		 ORDER BY domain_namespace.nspname, domain_type.typname`
)

// typeModifierPattern matches a parenthesised list of integer type modifiers, such as the
// (10,2) of numeric(10,2) or the (3) of timestamp(3) with time zone.
var typeModifierPattern = regexp.MustCompile(`\((\d+(?:\s*,\s*\d+)*)\)`)

// typeDescription is how the catalogue describes one attribute's or domain's type.
type typeDescription struct {
	// name is the pg_type.typname of the type, or of the element type for an array.
	name string

	// schema is the schema the type, or the array's element type, lives in.
	schema string

	// kind is the pg_type.typtype code of the type, or of the array's element type.
	kind string

	// formatted is the format_type text of the whole type, which carries its modifiers.
	formatted string

	// dimensions is the number of array dimensions, zero for a scalar.
	dimensions int
}

// scanDestinations returns the scan targets for the columns attributeTypeColumns selects.
//
// Returns []any which holds pointers to the description's fields in select order.
func (description *typeDescription) scanDestinations() []any {
	return []any{
		&description.name,
		&description.schema,
		&description.kind,
		&description.dimensions,
		&description.formatted,
	}
}

// typeKey identifies a type by schema and name.
type typeKey struct {
	// schema is the type's schema.
	schema string

	// name is the type's name.
	name string
}

// domainEntry is one domain and the type it is declared over.
type domainEntry struct {
	// base describes the type the domain is declared over.
	base typeDescription

	// notNull reports whether the domain is declared NOT NULL.
	notNull bool
}

// resolvedType is a column type after domains have been followed and the type normalised.
type resolvedType struct {
	// deepDomain names the domain reached when the chain of domains was nested more than
	// maxDomainDepth deep and so not followed to its end, or is nil.
	deepDomain *typeKey

	// sqlType is the normalised scalar type, or the element type of an array.
	sqlType sqlmodel.SQLType

	// dimensions is the number of array dimensions, zero for a scalar.
	dimensions int

	// notNull reports whether a NOT NULL domain constrains the value itself, rather than the
	// elements of an array of that domain.
	notNull bool
}

// typeResolver turns catalogue type descriptions into SQL types for one catalogue build.
type typeResolver struct {
	// typeNormaliser converts type names and modifiers to SQL types.
	typeNormaliser dialect.TypeNormaliserPort

	// domains maps each domain to the type it is declared over.
	domains map[typeKey]domainEntry

	// problems holds a warning for each column or field whose domains were nested too deeply
	// to follow to their end.
	problems []diag.SourceError
}

// resolve follows domains to their base type and normalises it.
//
// Enum and composite types the normaliser does not recognise are classified as such and
// carry their schema, as migration replay does; enum values are attached once the whole
// catalogue is known. A chain of domains nested more than maxDomainDepth deep is followed
// that far, the type reached there is normalised, and the domain it stopped at is
// reported in deepDomain.
//
// Takes description (typeDescription) which describes the column's declared type.
//
// Returns resolvedType which holds the element type, dimensions and domain nullability.
func (resolver *typeResolver) resolve(description typeDescription) resolvedType {
	dimensions := description.dimensions
	notNull := false
	depth := 0
	for ; description.kind == typeKindDomain && depth < maxDomainDepth; depth++ {
		domain, found := resolver.domains[typeKey{schema: description.schema, name: description.name}]
		if !found {
			break
		}
		if dimensions == 0 {
			notNull = notNull || domain.notNull
		}
		dimensions += domain.base.dimensions
		description = domain.base
	}

	var deepDomain *typeKey
	if depth == maxDomainDepth && description.kind == typeKindDomain {
		deepDomain = &typeKey{schema: description.schema, name: description.name}
	}

	sqlType := resolver.typeNormaliser.NormaliseTypeName(description.name, parseTypeModifiers(description.formatted)...)
	if sqlType.Category == sqlmodel.TypeCategoryUnknown {
		classifyUserDefinedType(&sqlType, description)
	}

	return resolvedType{
		deepDomain: deepDomain,
		sqlType:    sqlType,
		dimensions: min(dimensions, maxArrayDimensions),
		notNull:    notNull,
	}
}

// column builds a catalogue column from a described attribute type, warning when its
// domains were nested too deeply to follow to their end.
//
// Takes owner (string) which names the relation or composite type holding the column,
// qualified by its schema.
// Takes name (string) which is the column's name.
// Takes description (typeDescription) which describes the column's declared type.
// Takes nullable (bool) which reports whether the attribute itself permits NULL.
//
// Returns sqlmodel.Column which carries the resolved type, array shape and nullability.
func (resolver *typeResolver) column(owner, name string, description typeDescription, nullable bool) sqlmodel.Column {
	resolved := resolver.resolve(description)
	if resolved.deepDomain != nil {
		resolver.problems = append(resolver.problems, diagbuild.Unlocated(diag.CodeDepthLimit, diag.SeverityWarning,
			fmt.Sprintf("column %q of %s is declared over domains nested more than %d deep; its type was read from domain %s.%s, where the chain was no longer followed",
				name, owner, maxDomainDepth, resolved.deepDomain.schema, resolved.deepDomain.name)))
	}
	column := sqlmodel.NewColumn(name, resolved.sqlType, nullable && !resolved.notNull)
	column.IsArray = resolved.dimensions > 0
	column.ArrayDimensions = resolved.dimensions
	return column
}

// newTypeResolver loads the database's domains and returns a resolver over them.
//
// Returns *typeResolver which resolves column types for this build.
// Returns err (error) when the domain query, a scan, or closing the rows fails.
func (provider *Source) newTypeResolver(ctx context.Context) (resolver *typeResolver, err error) {
	rows, queryError := provider.database.QueryContext(ctx, domainsQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	domains := make(map[typeKey]domainEntry)
	for rows.Next() {
		var key typeKey
		var entry domainEntry
		destinations := append([]any{&key.schema, &key.name, &entry.notNull}, entry.base.scanDestinations()...)
		if scanError := rows.Scan(destinations...); scanError != nil {
			return nil, scanError
		}
		domains[key] = entry
	}

	if rowError := rows.Err(); rowError != nil {
		return nil, rowError
	}

	return &typeResolver{typeNormaliser: provider.typeNormaliser, domains: domains, problems: nil}, nil
}

// classifyUserDefinedType marks an unrecognised enum or composite type as such, naming it
// exactly as the catalogue does so its enum values can be found.
//
// Takes sqlType (*sqlmodel.SQLType) which is the normalised type to classify.
// Takes description (typeDescription) which says what kind of type it is.
func classifyUserDefinedType(sqlType *sqlmodel.SQLType, description typeDescription) {
	switch description.kind {
	case typeKindEnum:
		sqlType.Category = sqlmodel.TypeCategoryEnum
	case typeKindComposite:
		sqlType.Category = sqlmodel.TypeCategoryComposite
	default:
		return
	}
	sqlType.EngineName = description.name
	sqlType.Schema = description.schema
}

// parseTypeModifiers extracts the declared modifiers from format_type text, such as [10
// 2] from numeric(10,2) or [3] from timestamp(3) with time zone.
//
// Only modifiers the column declared appear, so an unconstrained timestamptz or varchar
// yields none, matching what migration replay records for the same declaration.
//
// Takes formatted (string) which is the format_type output.
//
// Returns []int which lists the modifiers in declaration order, or nil when there are
// none.
func parseTypeModifiers(formatted string) []int {
	match := typeModifierPattern.FindStringSubmatch(formatted)
	if match == nil {
		return nil
	}

	modifiers := make([]int, 0, strings.Count(match[1], ",")+1)
	for part := range strings.SplitSeq(match[1], ",") {
		value, parseError := strconv.Atoi(strings.TrimSpace(part))
		if parseError != nil {
			return nil
		}
		modifiers = append(modifiers, value)
	}
	return modifiers
}

// attachEnumValues fills in the values of every enum-typed column, view column and
// composite field from the enums the catalogue holds.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the introspected schemas.
func attachEnumValues(catalogue *sqlmodel.Catalogue) {
	for _, schema := range catalogue.Schemas {
		for _, table := range schema.Tables {
			attachColumnEnumValues(catalogue, table.Columns)
		}
		for _, view := range schema.Views {
			attachColumnEnumValues(catalogue, view.Columns)
		}
		for _, composite := range schema.CompositeTypes {
			attachColumnEnumValues(catalogue, composite.Fields)
		}
	}
}

// attachColumnEnumValues fills in the values of each enum-typed column in place.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the enums.
// Takes columns ([]sqlmodel.Column) which are updated in place.
func attachColumnEnumValues(catalogue *sqlmodel.Catalogue, columns []sqlmodel.Column) {
	for index := range columns {
		sqlType := &columns[index].SQLType
		if sqlType.Category != sqlmodel.TypeCategoryEnum {
			continue
		}
		schema, found := catalogue.Schemas[sqlType.Schema]
		if !found {
			continue
		}
		if enum, exists := schema.Enums[sqlType.EngineName]; exists {
			sqlType.EnumValues = enum.Values
		}
	}
}
