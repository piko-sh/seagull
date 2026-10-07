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
	"database/sql"
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// generatedStored is the pg_attribute.attgenerated code of a stored generated column.
	generatedStored = "s"

	// generatedVirtual is the pg_attribute.attgenerated code of a virtual generated column.
	generatedVirtual = "v"
)

var (
	// ErrNotConfigured reports that the source was created without a database connection or
	// type normaliser and so cannot introspect.
	ErrNotConfigured = errors.New("postgres catalogue source not configured")
)

// Source builds a schema catalogue by querying a live PostgreSQL database through
// pg_catalog and information_schema. It satisfies schema.SchemaSourcePort.
type Source struct {
	// database is the live PostgreSQL connection pool.
	database *sql.DB

	// typeNormaliser converts engine-specific type names to SQLType.
	typeNormaliser dialect.TypeNormaliserPort
}

// New creates a source that introspects a live PostgreSQL database.
//
// Takes database (*sql.DB) which is the connection pool to query.
// Takes typeNormaliser (dialect.TypeNormaliserPort) which converts the database's type
// names to SQL types; pass the dialect itself, such as dialect_engine_postgres.New().
//
// Returns *Source which is ready to build catalogues.
func New(database *sql.DB, typeNormaliser dialect.TypeNormaliserPort) *Source {
	return &Source{
		database:       database,
		typeNormaliser: typeNormaliser,
	}
}

// BuildCatalogue introspects the database and builds a catalogue. It leaves the search
// path and identifier rules unset; the generator takes them from the dialect when it
// adopts the catalogue.
//
// Covers tables, views, indexes, enums, composite types, functions, and extensions in
// every schema the role may use. System schemas, TimescaleDB's internal schemas, and the
// schemas and relations an extension owns are left out; partitioned parents, partitions,
// each naming the table it partitions, and foreign tables are described as tables, and a
// column the server hides from SELECT * is marked hidden. Array columns carry their
// element type and dimension count, domains resolve to the type they are declared over,
// and enum and composite columns are classified against the catalogue, matching what
// migration replay records for the same DDL. Each catalogue query runs once for all the
// schemas, so the number of queries does not grow with them.
//
// Returns *sqlmodel.Catalogue which holds the introspected schema.
// Returns []diag.SourceError which warns about each column whose type is declared over
// domains nested deeper than the resolver follows, or is nil.
// Returns error when the provider lacks a database or type normaliser (wrapping
// ErrNotConfigured), when any introspection query fails, or when ctx ends, in which case
// it wraps the context's error and cause.
func (provider *Source) BuildCatalogue(
	ctx context.Context,
) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	if provider.database == nil || provider.typeNormaliser == nil {
		return nil, nil, fmt.Errorf("building catalogue: %w", ErrNotConfigured)
	}

	catalogue := &sqlmodel.Catalogue{
		DefaultSchema: "public",
		SearchPath:    nil,
		Identifiers:   sqlmodel.IdentifierCase{},
		Schemas:       make(map[string]*sqlmodel.Schema),
		Extensions:    make(map[string]struct{}),
	}

	schemaNames, schemaError := rowscan.Strings(ctx, provider.database, schemasQuery)
	if schemaError != nil {
		return nil, nil, fmt.Errorf("listing schemas: %w", rowscan.WithCause(ctx, schemaError))
	}
	for _, schemaName := range schemaNames {
		catalogue.Schemas[schemaName] = newSchema(schemaName)
	}

	resolver, domainError := provider.newTypeResolver(ctx)
	if domainError != nil {
		return nil, nil, fmt.Errorf("listing domains: %w", rowscan.WithCause(ctx, domainError))
	}

	for _, step := range provider.steps(catalogue, resolver) {
		if stepError := step.run(ctx); stepError != nil {
			return nil, nil, fmt.Errorf("introspecting %s: %w", step.subject, rowscan.WithCause(ctx, stepError))
		}
	}
	attachEnumValues(catalogue)

	if hiddenError := provider.markHiddenColumns(ctx, catalogue); hiddenError != nil {
		return nil, nil, fmt.Errorf("introspecting hidden columns: %w", rowscan.WithCause(ctx, hiddenError))
	}

	if extensionError := provider.introspectExtensions(ctx, catalogue); extensionError != nil {
		return nil, nil, fmt.Errorf("introspecting extensions: %w", rowscan.WithCause(ctx, extensionError))
	}

	return catalogue, resolver.problems, nil
}

// steps lists the queries that fill the covered schemas, in the order they run: tables
// and views are listed before their columns, keys and indexes are read.
//
// Takes catalogue (*sqlmodel.Catalogue) which receives what the queries read.
// Takes resolver (*typeResolver) which resolves column and field types.
//
// Returns []introspectionStep which holds the steps in order.
func (provider *Source) steps(catalogue *sqlmodel.Catalogue, resolver *typeResolver) []introspectionStep {
	return []introspectionStep{
		{subject: "tables", run: func(ctx context.Context) error { return provider.listTables(ctx, catalogue) }},
		{subject: "views", run: func(ctx context.Context) error { return provider.listViews(ctx, catalogue) }},
		{subject: "columns", run: func(ctx context.Context) error { return provider.schemaColumns(ctx, resolver, catalogue) }},
		{subject: "constraints", run: func(ctx context.Context) error { return provider.schemaConstraints(ctx, catalogue) }},
		{subject: "indexes", run: func(ctx context.Context) error { return provider.schemaIndexes(ctx, catalogue) }},
		{subject: "enums", run: func(ctx context.Context) error { return provider.introspectEnums(ctx, catalogue) }},
		{subject: "composite types", run: func(ctx context.Context) error {
			return provider.introspectCompositeTypes(ctx, resolver, catalogue)
		}},
		{subject: "functions", run: func(ctx context.Context) error { return provider.introspectFunctions(ctx, catalogue) }},
	}
}

// listTables records the covered schemas' tables, as tablesQuery selects them, in their
// schemas.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the covered schemas.
//
// Returns err (error) when the query, a scan, or closing the rows fails.
func (provider *Source) listTables(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
) (err error) {
	rows, queryError := provider.database.QueryContext(ctx, tablesQuery)
	if queryError != nil {
		return queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var schemaName, name, parentSchema, parentName string
		if scanError := rows.Scan(&schemaName, &name, &parentSchema, &parentName); scanError != nil {
			return scanError
		}
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		entry := listedTable{partitionOf: nil, name: name}
		if parentName != "" {
			entry.partitionOf = &sqlmodel.TableName{Schema: parentSchema, Name: parentName}
		}
		schema.Tables[entry.name] = newTable(schema.Name, entry)
	}

	return rows.Err()
}

// introspectionStep is one catalogue query and what it fills in.
type introspectionStep struct {
	// run runs the query and records its rows in the catalogue.
	run func(ctx context.Context) error

	// subject names what the query reads, for its error.
	subject string
}

// listedTable is one table tablesQuery lists.
type listedTable struct {
	// partitionOf names the table it partitions, or is nil for a table that is no partition.
	partitionOf *sqlmodel.TableName

	// name is the table's name.
	name string
}

// columnRow mirrors one row of columnsQuery.
type columnRow struct {
	// columnName is the column's identifier.
	columnName string

	// generated is the pg_attribute.attgenerated code: "s" for stored, "v" for virtual,
	// empty for an ordinary column.
	generated string

	// typeDescription describes the column's declared type.
	typeDescription typeDescription

	// notNull reports whether the column is declared NOT NULL.
	notNull bool

	// hasDefault reports whether the column has a DEFAULT expression.
	hasDefault bool

	// isIdentity reports whether the column is an identity column.
	isIdentity bool
}

// scanColumnRow decodes one column row.
//
// Takes rows (*sql.Rows) which is positioned on the row to scan.
// Takes prefix (...any) which holds destinations for metadata columns preceding the
// column description.
//
// Returns columnRow which is the decoded row.
// Returns error when scanning fails.
func scanColumnRow(rows *sql.Rows, prefix ...any) (columnRow, error) {
	var row columnRow

	destinations := append(
		append(prefix, []any{&row.columnName, &row.notNull, &row.hasDefault, &row.isIdentity, &row.generated}...),
		row.typeDescription.scanDestinations()...,
	)
	if scanError := rows.Scan(destinations...); scanError != nil {
		return columnRow{}, scanError
	}
	return row, nil
}

// columnFromRow builds a column from its decoded row, resolving its SQL type.
//
// Takes resolver (*typeResolver) which resolves the column's type.
// Takes owner (string) which names the relation holding the column, for a diagnostic.
// Takes row (columnRow) which is the decoded row.
//
// Returns sqlmodel.Column which is the column.
func columnFromRow(resolver *typeResolver, owner string, row columnRow) sqlmodel.Column {
	column := resolver.column(owner, row.columnName, row.typeDescription, !row.notNull)
	column.HasDefault = row.hasDefault || row.isIdentity

	switch row.generated {
	case generatedStored:
		column.IsGenerated = true
		column.GeneratedKind = sqlmodel.GeneratedKindStored
	case generatedVirtual:
		column.IsGenerated = true
		column.GeneratedKind = sqlmodel.GeneratedKindVirtual
	}

	return column
}

// newSchema returns an empty schema ready to receive what the catalogue queries read.
//
// Takes name (string) which is the schema's name.
//
// Returns *sqlmodel.Schema which is the empty schema.
func newSchema(name string) *sqlmodel.Schema {
	return &sqlmodel.Schema{
		Name:           name,
		Tables:         make(map[string]*sqlmodel.Table),
		Views:          make(map[string]*sqlmodel.View),
		Enums:          make(map[string]*sqlmodel.Enum),
		Functions:      make(map[string][]*sqlmodel.Function),
		CompositeTypes: make(map[string]*sqlmodel.CompositeType),
		Sequences:      make(map[string]*sqlmodel.Sequence),
		Domains:        make(map[string]*sqlmodel.Domain),
	}
}

// newTable returns a listed table before its columns, keys and indexes are read.
//
// Takes schemaName (string) which is the schema holding the table.
// Takes entry (listedTable) which is the table as tablesQuery lists it.
//
// Returns *sqlmodel.Table which is the table.
func newTable(schemaName string, entry listedTable) *sqlmodel.Table {
	return &sqlmodel.Table{
		Name:              entry.name,
		Schema:            schemaName,
		PartitionOf:       entry.partitionOf,
		Indexes:           []sqlmodel.Index{},
		Columns:           nil,
		Comment:           "",
		Constraints:       nil,
		IsVirtual:         false,
		IsWithoutRowID:    false,
		Origin:            sqlmodel.MigrationOrigin{},
		PrimaryKey:        nil,
		VirtualModuleName: "",
	}
}
