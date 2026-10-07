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

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	sqlite_lexical "seagull.sh/seagull/internal/dialect/sqlite/lexical"
	"seagull.sh/seagull/internal/schema/internal/rowscan"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// schemaMain is the SQLite default schema name.
	schemaMain = "main"

	// hiddenColumn marks a column PRAGMA table_xinfo reports as hidden from SELECT *, such
	// as the rank column and the column named after the table of an FTS5 table.
	hiddenColumn = 1

	// hiddenVirtualColumn marks a generated column stored as a virtual expression in PRAGMA
	// table_xinfo output.
	hiddenVirtualColumn = 2

	// hiddenStoredColumn marks a generated column whose value is stored on disk in PRAGMA
	// table_xinfo output.
	hiddenStoredColumn = 3

	// indexOriginPrimaryKey is the PRAGMA index_list origin reported for the automatic index
	// that backs a PRIMARY KEY constraint.
	indexOriginPrimaryKey = "pk"

	// rowidAliasType is the declared type that makes a single-column primary key an alias
	// for the rowid.
	rowidAliasType = "integer"

	// rowidEngineName is the engine type of a rowid alias, the signed 64-bit rowid, named as
	// the SQLite dialect names it.
	rowidEngineName = "bigint"

	// tableKindVirtual is the pragma_table_list type of a virtual table.
	tableKindVirtual = "virtual"

	// moduleFTS5 is the module name of an FTS5 full-text table.
	moduleFTS5 = "fts5"

	// fts5RankColumn names the hidden column an FTS5 table ranks its matches by.
	fts5RankColumn = "rank"

	// fts5TextType is the type of every column an FTS5 table declares, and of the hidden
	// column named after the table: FTS5 declares them with no type and holds text.
	fts5TextType = "text"

	// fts5RankType is the type of an FTS5 table's hidden rank column.
	fts5RankType = "real"

	// userRelations keeps the rows of pragma_table_list, aliased list, that name the main
	// schema's own relations, leaving out SQLite's internal sqlite_ tables. The underscore
	// is escaped because LIKE otherwise treats it as a single-character wildcard and would
	// also hide user tables such as "sqliteapp".
	userRelations = `list."schema" = 'main' AND list.name NOT LIKE 'sqlite\_%' ESCAPE '\'`

	// listTablesQuery selects the user tables and virtual tables, each with whether it is
	// WITHOUT ROWID and whether it is STRICT, and a virtual table's CREATE VIRTUAL TABLE
	// statement. The shadow tables a virtual table keeps its data in are left out.
	listTablesQuery = `SELECT list.name, list."type", list."wr", list."strict",
			COALESCE(CASE WHEN list."type" = 'virtual' THEN master.sql END, '')
		FROM pragma_table_list AS list
		LEFT JOIN sqlite_master AS master ON master."type" = 'table' AND master.name = list.name
		WHERE ` + userRelations + ` AND list."type" IN ('table', 'virtual')
		ORDER BY list.name`

	// listViewsQuery selects user views with the CREATE VIEW statement SQLite keeps for
	// each.
	listViewsQuery = "SELECT name, sql FROM sqlite_master WHERE type = 'view' ORDER BY name"

	// relationColumnsQuery selects every column of every table, virtual table and view,
	// hidden ones included, through the table-valued form of PRAGMA table_xinfo joined to
	// the relations it describes, so one statement reads them all.
	relationColumnsQuery = `SELECT list.name, info.cid, info.name, info."type", info."notnull", info.dflt_value, info.pk, info.hidden
		FROM pragma_table_list AS list
		JOIN pragma_table_xinfo(list.name, list."schema") AS info
		WHERE ` + userRelations + ` AND list."type" IN ('table', 'virtual', 'view')
		ORDER BY list.name, info.cid`

	// tableIndexesQuery selects every index of every table together with its key columns.
	//
	// Joining the table-valued PRAGMA functions in one statement avoids issuing a second
	// query while the first result set is still open, which would deadlock a pool limited to
	// one connection. Expression key columns have a NULL name.
	tableIndexesQuery = `SELECT list.name, index_list.name, index_list."unique", index_list.origin, index_info.name
		FROM pragma_table_list AS list
		JOIN pragma_index_list(list.name, list."schema") AS index_list
		LEFT JOIN pragma_index_info(index_list.name, list."schema") AS index_info
		WHERE ` + userRelations + ` AND list."type" = 'table'
		ORDER BY list.name, index_list.seq, index_info.seqno`
)

var (
	// ErrNotConfigured reports that the source was created without a database connection or
	// type normaliser and so cannot introspect.
	ErrNotConfigured = errors.New("sqlite catalogue source not configured")
)

// Source builds a schema catalogue by querying a live SQLite database through PRAGMA
// commands. It satisfies schema.SchemaSourcePort.
type Source struct {
	// database is the SQLite connection used to issue PRAGMA queries.
	database *sql.DB

	// typeNormaliser converts raw SQLite type strings to structured SQLType values.
	typeNormaliser dialect.TypeNormaliserPort
}

// New creates a source that introspects a live SQLite database.
//
// Takes database (*sql.DB) which is the connection pool to query.
// Takes typeNormaliser (dialect.TypeNormaliserPort) which converts the database's type
// names to SQL types; pass the dialect itself, such as dialect_engine_sqlite.New().
//
// Returns *Source which is ready to build catalogues.
func New(database *sql.DB, typeNormaliser dialect.TypeNormaliserPort) *Source {
	return &Source{
		database:       database,
		typeNormaliser: typeNormaliser,
	}
}

// BuildCatalogue introspects the SQLite database and builds a schema catalogue. It leaves
// the search path and identifier rules unset; the generator takes them from the dialect
// when it adopts the catalogue.
//
// Tables, virtual tables and views are read with a fixed number of queries however many
// there are. A virtual table carries its module, a WITHOUT ROWID table says so, the
// shadow tables a virtual table keeps its data in are left out, and a column SQLite hides
// from SELECT * is marked hidden, matching what migration replay records for the same
// DDL.
//
// Returns *sqlmodel.Catalogue which describes tables, views, and indexes.
// Returns []diag.SourceError which lists per-object diagnostics, always nil for the
// SQLite provider.
// Returns error when the source lacks a database or type normaliser (wrapping
// ErrNotConfigured), when a PRAGMA or introspection query fails, or when ctx ends, in
// which case it wraps the context's error and cause.
func (provider *Source) BuildCatalogue(
	ctx context.Context,
) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	if provider.database == nil || provider.typeNormaliser == nil {
		return nil, nil, fmt.Errorf("building catalogue: %w", ErrNotConfigured)
	}

	read, readError := provider.read(ctx)
	if readError != nil {
		return nil, nil, rowscan.WithCause(ctx, readError)
	}

	catalogue := newCatalogue()
	schema := catalogue.Schemas[schemaMain]

	for position, listed := range read.tables {
		if stopped := rowscan.Stopped(ctx); stopped != nil {
			return nil, nil, fmt.Errorf("introspection stopped after %d of %d tables: %w", position, len(read.tables), stopped)
		}
		schema.Tables[listed.name] = provider.assembleTable(listed, read.columns[listed.name], read.indexes[listed.name])
	}

	for position, listed := range read.views {
		if stopped := rowscan.Stopped(ctx); stopped != nil {
			return nil, nil, fmt.Errorf("introspection stopped after %d of %d views: %w", position, len(read.views), stopped)
		}
		schema.Views[listed.name] = provider.assembleView(listed, read.columns[listed.name])
	}

	return catalogue, nil, nil
}

// read runs the catalogue queries, each once for the whole database.
//
// Returns snapshot which holds the rows read.
// Returns error when a query, a scan, row iteration, or closing the rows fails.
func (provider *Source) read(ctx context.Context) (snapshot, error) {
	tables, tableError := provider.listTables(ctx)
	if tableError != nil {
		return snapshot{}, fmt.Errorf("listing tables: %w", tableError)
	}
	views, viewError := provider.listViews(ctx)
	if viewError != nil {
		return snapshot{}, fmt.Errorf("listing views: %w", viewError)
	}
	columns, columnError := provider.relationColumns(ctx)
	if columnError != nil {
		return snapshot{}, fmt.Errorf("reading columns: %w", columnError)
	}
	indexes, indexError := provider.tableIndexes(ctx)
	if indexError != nil {
		return snapshot{}, fmt.Errorf("reading indexes: %w", indexError)
	}
	return snapshot{columns: columns, indexes: indexes, tables: tables, views: views}, nil
}

// listTables returns the user tables and virtual tables in the SQLite database.
//
// Returns tables ([]listedTable) which holds the tables in name order.
// Returns err (error) when the query, a scan, row iteration, or closing the rows fails.
func (provider *Source) listTables(
	ctx context.Context,
) (tables []listedTable, err error) {
	rows, queryError := provider.database.QueryContext(ctx, listTablesQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var table listedTable
		var withoutRowID, strict int
		if scanError := rows.Scan(&table.name, &table.kind, &withoutRowID, &strict, &table.statement); scanError != nil {
			return nil, scanError
		}
		table.withoutRowID = withoutRowID != 0
		table.strict = strict != 0
		tables = append(tables, table)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}
	return tables, nil
}

// listViews returns the user views in the SQLite database with their CREATE VIEW
// statements.
//
// Returns []listedView which holds the views in alphabetical order.
// Returns error when the catalogue query, a scan, or row iteration fails.
func (provider *Source) listViews(
	ctx context.Context,
) (views []listedView, err error) {
	rows, queryError := provider.database.QueryContext(ctx, listViewsQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	for rows.Next() {
		var view listedView
		var statement sql.NullString
		if scanError := rows.Scan(&view.name, &statement); scanError != nil {
			return nil, scanError
		}
		view.statement = statement.String
		views = append(views, view)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}
	return views, nil
}

// relationColumns reads the PRAGMA table_xinfo rows of every table, virtual table and
// view in one query.
//
// Returns columns (map[string][]pragmaColumn) which holds each relation's rows in column
// order, keyed by relation name.
// Returns err (error) when the query, a scan, row iteration, or closing the rows fails.
func (provider *Source) relationColumns(
	ctx context.Context,
) (columns map[string][]pragmaColumn, err error) {
	rows, queryError := provider.database.QueryContext(ctx, relationColumnsQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	columns = make(map[string][]pragmaColumn)
	for rows.Next() {
		var relation string
		var row pragmaColumn
		scanError := rows.Scan(&relation, &row.columnID, &row.name, &row.typeName, &row.notNull, &row.defaultValue, &row.primaryKeyPosition, &row.hidden)
		if scanError != nil {
			return nil, scanError
		}
		columns[relation] = append(columns[relation], row)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}
	return columns, nil
}

// tableIndexes reads every index of every table and its named key columns in one query.
//
// Every index and its key columns are read through one query whose rows are fully
// consumed before it returns, so no second statement is issued while a result set is
// open. That matters because the SQLite drivers cap the pool at one connection, where a
// nested query would wait forever for the connection the outer rows still hold.
//
// Returns indexes (map[string][]sqlmodel.Index) which holds each table's indexes, keyed
// by table name, omitting indexes whose keys are all expressions.
// Returns err (error) when the query, a scan, row iteration, or closing the rows fails.
func (provider *Source) tableIndexes(
	ctx context.Context,
) (indexes map[string][]sqlmodel.Index, err error) {
	rows, queryError := provider.database.QueryContext(ctx, tableIndexesQuery)
	if queryError != nil {
		return nil, queryError
	}
	defer rowscan.Close(rows, &err)

	indexes = make(map[string][]sqlmodel.Index)
	positionByName := map[[2]string]int{}
	for rows.Next() {
		var tableName, indexName, origin string
		var unique int
		var columnName sql.NullString
		if scanError := rows.Scan(&tableName, &indexName, &unique, &origin, &columnName); scanError != nil {
			return nil, scanError
		}

		key := [2]string{tableName, indexName}
		position, seen := positionByName[key]
		if !seen {
			position = len(indexes[tableName])
			positionByName[key] = position
			indexes[tableName] = append(indexes[tableName], sqlmodel.Index{
				Name:      indexName,
				Columns:   nil,
				IsUnique:  unique != 0,
				IsPrimary: origin == indexOriginPrimaryKey,
				Origin:    sqlmodel.MigrationOrigin{},
			})
		}
		if columnName.Valid {
			indexes[tableName][position].Columns = append(indexes[tableName][position].Columns, columnName.String)
		}
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}

	for tableName, tableIndexes := range indexes {
		indexes[tableName] = slices.DeleteFunc(tableIndexes, func(index sqlmodel.Index) bool {
			return len(index.Columns) == 0
		})
	}
	return indexes, nil
}

// assembleTable builds a Table descriptor from the rows read for it.
//
// Takes listed (listedTable) which is the table as pragma_table_list lists it.
// Takes rows ([]pragmaColumn) which are its PRAGMA table_xinfo rows.
// Takes indexes ([]sqlmodel.Index) which are its indexes.
//
// Returns *sqlmodel.Table which describes columns, primary key, indexes and table kind.
func (provider *Source) assembleTable(listed listedTable, rows []pragmaColumn, indexes []sqlmodel.Index) *sqlmodel.Table {
	module := ""
	if listed.kind == tableKindVirtual {
		module = virtualModule(listed.statement)
	}
	columns, primaryKeyColumns, declaredTypes := provider.buildColumns(rows, module)

	if listed.withoutRowID || listed.strict {
		markKeyColumnsNotNull(columns, primaryKeyColumns)
	}
	applyRowidAlias(columns, primaryKeyColumns, declaredTypes, indexes)

	return &sqlmodel.Table{
		Name:              listed.name,
		Schema:            schemaMain,
		Columns:           columns,
		PrimaryKey:        primaryKeyColumns,
		Indexes:           indexes,
		Comment:           "",
		VirtualModuleName: module,
		Constraints:       nil,
		PartitionOf:       nil,
		Origin:            sqlmodel.MigrationOrigin{},
		IsVirtual:         listed.kind == tableKindVirtual,
		IsWithoutRowID:    listed.withoutRowID,
	}
}

// assembleView builds a View descriptor from the rows read for it.
//
// PRAGMA columns are initially nullable; analysing the definition can refine their types.
//
// Takes listed (listedView) which names the view and holds its CREATE VIEW statement.
// Takes rows ([]pragmaColumn) which are its PRAGMA table_xinfo rows.
//
// Returns *sqlmodel.View which describes the view.
func (provider *Source) assembleView(listed listedView, rows []pragmaColumn) *sqlmodel.View {
	columns, _, _ := provider.buildColumns(rows, "")
	return &sqlmodel.View{
		Name:       listed.name,
		Schema:     schemaMain,
		Columns:    columns,
		Definition: viewDefinition(listed.statement),
		Comment:    "",
		Origin:     sqlmodel.MigrationOrigin{},
	}
}

// buildColumns builds the columns, primary key and declared types of a relation.
//
// Takes rows ([]pragmaColumn) which are the relation's PRAGMA table_xinfo rows in column
// order.
// Takes module (string) which is a virtual table's lower-case module name, or empty.
//
// Returns columns ([]sqlmodel.Column) which describes each column.
// Returns primaryKey ([]string) which contains primary key column names in order.
// Returns declaredTypes (map[string]string) which maps each column to its declared type.
func (provider *Source) buildColumns(
	rows []pragmaColumn,
	module string,
) (columns []sqlmodel.Column, primaryKey []string, declaredTypes map[string]string) {
	primaryKeyByPosition := map[int]string{}
	declaredTypes = make(map[string]string, len(rows))
	columns = make([]sqlmodel.Column, 0, len(rows))
	for _, row := range rows {
		column := provider.columnFromPragma(row, module)
		columns = append(columns, column)
		declaredTypes[column.Name] = row.typeName
		if row.primaryKeyPosition > 0 {
			primaryKeyByPosition[row.primaryKeyPosition] = column.Name
		}
	}
	return columns, orderedPrimaryKeyColumns(primaryKeyByPosition), declaredTypes
}

// columnFromPragma builds a column descriptor from one PRAGMA table_xinfo row.
//
// A column has a default only when it declares one; the rowid alias, whose value SQLite
// assigns, is marked later by applyRowidAlias. A column is NOT NULL only when declared
// so: SQLite's legacy behaviour lets a rowid table store NULL in a primary key that is
// not the rowid alias, and assembleTable marks the keys SQLite holds NOT NULL. A column
// SQLite hides from SELECT * is marked hidden, and an FTS5 table's columns are typed as
// FTS5 holds them.
//
// Takes row (pragmaColumn) which is the decoded row.
// Takes module (string) which is a virtual table's lower-case module name, or empty.
//
// Returns sqlmodel.Column which describes the column.
func (provider *Source) columnFromPragma(row pragmaColumn, module string) sqlmodel.Column {
	typeName, modifiers := splitDeclaredType(columnType(row, module))
	sqlType := provider.typeNormaliser.NormaliseTypeName(typeName, modifiers...)
	column := sqlmodel.NewColumn(row.name, sqlType, row.notNull == 0)
	column.HasDefault = row.defaultValue.Valid
	column.IsGenerated, column.GeneratedKind = classifyGeneratedColumn(row.hidden)
	column.Hidden = row.hidden == hiddenColumn
	if isFTS5Rank(row, module) {
		column.IsGenerated, column.GeneratedKind = true, sqlmodel.GeneratedKindVirtual
	}
	return column
}

// listedTable is a table or virtual table as pragma_table_list lists it.
type listedTable struct {
	// name is the table's name.
	name string

	// kind is the pragma_table_list type: "table" or "virtual".
	kind string

	// statement is the CREATE VIRTUAL TABLE statement of a virtual table, or empty.
	statement string

	// withoutRowID reports whether the table is WITHOUT ROWID.
	withoutRowID bool

	// strict reports whether the table is STRICT.
	strict bool
}

// listedView is a view as sqlite_master lists it.
type listedView struct {
	// name is the view's name.
	name string

	// statement is the CREATE VIEW statement SQLite keeps for it.
	statement string
}

// pragmaColumn is one row of PRAGMA table_xinfo.
type pragmaColumn struct {
	// name is the column name.
	name string

	// typeName is the declared type as written.
	typeName string

	// defaultValue is the column's declared DEFAULT expression, if any.
	defaultValue sql.NullString

	// columnID is the column's position in the table.
	columnID int

	// notNull is 1 when the column is declared NOT NULL.
	notNull int

	// primaryKeyPosition is the column's 1-based position in the primary key, or 0.
	primaryKeyPosition int

	// hidden is SQLite's hidden-column flag: 1 for a column hidden from SELECT *, 2 and 3
	// for generated columns.
	hidden int
}

// snapshot holds what the catalogue queries read, before it is assembled into tables and
// views.
type snapshot struct {
	// columns holds each relation's PRAGMA table_xinfo rows, keyed by relation name.
	columns map[string][]pragmaColumn

	// indexes holds each table's indexes, keyed by table name.
	indexes map[string][]sqlmodel.Index

	// tables lists the tables and virtual tables in name order.
	tables []listedTable

	// views lists the views in name order.
	views []listedView
}

// markKeyColumnsNotNull marks every primary-key column NOT NULL.
//
// Takes columns ([]sqlmodel.Column) which are updated in place.
// Takes primaryKey ([]string) which names the primary key's columns.
func markKeyColumnsNotNull(columns []sqlmodel.Column, primaryKey []string) {
	for position := range columns {
		if slices.Contains(primaryKey, columns[position].Name) {
			columns[position].Nullable = false
		}
	}
}

// viewDefinition returns the SELECT of a CREATE VIEW statement: the text after the AS
// that follows the view's name and its optional column list, read under SQLite's lexical
// rules so an AS inside a quoted name or a comment is not taken for it.
//
// Takes statement (string) which is the CREATE VIEW statement.
//
// Returns string which is the SELECT, or empty when the statement has no such AS.
func viewDefinition(statement string) string {
	scanner := sqllex.NewScanner(sqlite_lexical.Rules(), statement)
	depth := 0
	for {
		token, ok := scanner.NextSignificant()
		if !ok {
			return ""
		}
		switch {
		case token.Kind == sqllex.KindPunctuation && token.Raw == "(":
			depth++
		case token.Kind == sqllex.KindPunctuation && token.Raw == ")":
			depth--
		case depth == 0 && token.Kind == sqllex.KindWord && strings.EqualFold(token.Raw, "AS"):
			return strings.TrimSpace(statement[token.End:])
		}
	}
}

// splitDeclaredType splits a declared type such as VARCHAR(100) or DECIMAL(10, 2) into
// its name and its numeric modifiers, as replaying the CREATE TABLE reads them, so the
// normaliser gets the length, precision and scale.
//
// A type without modifiers, or whose parenthesised part is not a list of integers, is
// returned whole.
//
// Takes declared (string) which is the declared type as PRAGMA table_xinfo reports it.
//
// Returns string which is the type's name.
// Returns []int which holds its modifiers, or nil.
func splitDeclaredType(declared string) (string, []int) {
	declared = strings.TrimSpace(declared)
	name, arguments, found := strings.Cut(declared, "(")
	arguments, closed := strings.CutSuffix(strings.TrimSpace(arguments), ")")
	if !found || !closed {
		return declared, nil
	}
	var modifiers []int
	for argument := range strings.SplitSeq(arguments, ",") {
		modifier, err := strconv.Atoi(strings.TrimSpace(argument))
		if err != nil {
			return declared, nil
		}
		modifiers = append(modifiers, modifier)
	}
	return strings.TrimSpace(name), modifiers
}

// applyRowidAlias marks an INTEGER PRIMARY KEY as SQLite's rowid alias.
//
// The alias is a defaulted, NOT NULL 64-bit integer. WITHOUT ROWID tables and other
// primary keys are left unchanged.
//
// Takes columns ([]sqlmodel.Column) which are updated in place.
// Takes primaryKey ([]string) which names the primary key's columns.
// Takes declaredTypes (map[string]string) which maps each column to its declared type.
// Takes indexes ([]sqlmodel.Index) which are the table's indexes.
func applyRowidAlias(columns []sqlmodel.Column, primaryKey []string, declaredTypes map[string]string, indexes []sqlmodel.Index) {
	if len(primaryKey) != 1 || !strings.EqualFold(strings.TrimSpace(declaredTypes[primaryKey[0]]), rowidAliasType) {
		return
	}
	if slices.ContainsFunc(indexes, func(index sqlmodel.Index) bool { return index.IsPrimary }) {
		return
	}
	for position := range columns {
		if columns[position].Name == primaryKey[0] {
			columns[position].SQLType.EngineName = rowidEngineName
			columns[position].HasDefault = true
			columns[position].Nullable = false
		}
	}
}

// orderedPrimaryKeyColumns returns the primary-key column names ordered by their 1-based
// position within the key, or nil when the table has no primary key.
//
// Takes byPosition (map[int]string) which maps a 1-based key position to its column name.
//
// Returns []string which holds the column names in key order.
func orderedPrimaryKeyColumns(byPosition map[int]string) []string {
	if len(byPosition) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(byPosition))
	for _, position := range slices.Sorted(maps.Keys(byPosition)) {
		ordered = append(ordered, byPosition[position])
	}
	return ordered
}

// classifyGeneratedColumn maps a PRAGMA table_xinfo hidden flag to its generated state.
//
// SQLite reports hiddenVirtualColumn for a VIRTUAL generated column and
// hiddenStoredColumn for a STORED one. Any other flag denotes an ordinary column.
//
// Takes hidden (int) which is the hidden flag from PRAGMA table_xinfo.
//
// Returns bool which is true when the column is a generated column.
// Returns sqlmodel.GeneratedKind which distinguishes STORED from VIRTUAL, or
// GeneratedKindNone for an ordinary column.
func classifyGeneratedColumn(hidden int) (bool, sqlmodel.GeneratedKind) {
	switch hidden {
	case hiddenStoredColumn:
		return true, sqlmodel.GeneratedKindStored
	case hiddenVirtualColumn:
		return true, sqlmodel.GeneratedKindVirtual
	default:
		return false, sqlmodel.GeneratedKindNone
	}
}

// columnType returns the type a column is read as: its declared type, except in an FTS5
// table, whose module declares its columns with no type. FTS5 holds text in each column
// it declares and in the hidden column named after the table, and a real in its hidden
// rank column, as migration replay records them.
//
// Takes row (pragmaColumn) which is the column's PRAGMA table_xinfo row.
// Takes module (string) which is a virtual table's lower-case module name, or empty.
//
// Returns string which is the type to normalise.
func columnType(row pragmaColumn, module string) string {
	if module != moduleFTS5 || strings.TrimSpace(row.typeName) != "" {
		return row.typeName
	}
	if isFTS5Rank(row, module) {
		return fts5RankType
	}
	return fts5TextType
}

// isFTS5Rank reports whether a column is the hidden rank column of an FTS5 table, which
// SQLite computes for each match.
//
// Takes row (pragmaColumn) which is the column's PRAGMA table_xinfo row.
// Takes module (string) which is a virtual table's lower-case module name, or empty.
//
// Returns bool which is true for the rank column.
func isFTS5Rank(row pragmaColumn, module string) bool {
	return module == moduleFTS5 && row.hidden == hiddenColumn && strings.EqualFold(row.name, fts5RankColumn)
}

// virtualModule returns the module of a CREATE VIRTUAL TABLE statement: the name after
// its USING, read under SQLite's lexical rules so a USING inside a quoted name or a
// comment is not taken for it, and lower-cased as SQLite matches module names without
// regard to case.
//
// Takes statement (string) which is the CREATE VIRTUAL TABLE statement.
//
// Returns string which is the lower-case module name, or empty when the statement names
// none.
func virtualModule(statement string) string {
	scanner := sqllex.NewScanner(sqlite_lexical.Rules(), statement)
	for {
		token, ok := scanner.NextSignificant()
		if !ok {
			return ""
		}
		if !token.IsWord("USING") {
			continue
		}
		module, found := scanner.NextSignificant()
		if !found {
			return ""
		}
		return strings.ToLower(identifierText(module))
	}
}

// identifierText returns the name a word or quoted identifier token spells, without its
// quotes and with a doubled closing quote read as one.
//
// Takes token (sqllex.Token) which is the token.
//
// Returns string which is the name, or empty for a token that is neither.
func identifierText(token sqllex.Token) string {
	switch token.Kind {
	case sqllex.KindWord:
		return token.Raw
	case sqllex.KindQuotedIdentifier:
		if len(token.Raw) < 2 {
			return ""
		}
		closing := token.Raw[len(token.Raw)-1:]
		inner := token.Raw[1 : len(token.Raw)-1]
		if closing == "]" {
			return inner
		}
		return strings.ReplaceAll(inner, closing+closing, closing)
	default:
		return ""
	}
}

// newCatalogue returns an empty catalogue holding the main schema.
//
// Returns *sqlmodel.Catalogue which is ready to receive the introspected relations.
func newCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: schemaMain,
		SearchPath:    nil,
		Identifiers:   sqlmodel.IdentifierCase{},
		Schemas: map[string]*sqlmodel.Schema{
			schemaMain: {
				Name:           schemaMain,
				Tables:         make(map[string]*sqlmodel.Table),
				Views:          make(map[string]*sqlmodel.View),
				Enums:          make(map[string]*sqlmodel.Enum),
				Functions:      make(map[string][]*sqlmodel.Function),
				CompositeTypes: make(map[string]*sqlmodel.CompositeType),
				Sequences:      make(map[string]*sqlmodel.Sequence),
				Domains:        make(map[string]*sqlmodel.Domain),
			},
		},
		Extensions: make(map[string]struct{}),
	}
}
