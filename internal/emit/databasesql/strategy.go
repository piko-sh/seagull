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

package databasesql

import (
	"go/ast"
	"slices"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/valuereading"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// importSQL is the import path of database/sql.
	importSQL = "database/sql"

	// importContext is the import path of context.
	importContext = "context"

	// sqlLists is how the generated code writes a slice's list for database/sql: one
	// placeholder per element, except in a form that binds by name.
	sqlLists = emitkit.ListExpand
)

var (
	// convertedOverrides lists SQL categories whose driver values can require Go override
	// conversion.
	convertedOverrides = []sqlmodel.SQLTypeCategory{
		sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryJSON, sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryBoolean,
		sqlmodel.TypeCategoryNetwork, sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryStruct,
	}

	// sqlConnection says how the generated code calls a database/sql connection.
	sqlConnection = gogen.Connection{
		ExecResult:         &sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "Result"},
		Query:              "QueryContext",
		QueryRow:           "QueryRowContext",
		Exec:               "ExecContext",
		Reader:             goast.IdentReader,
		Writer:             goast.IdentWriter,
		CountScan:          "",
		NoRows:             sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "ErrNoRows"},
		RowsAffectedErrors: true,
	}

	// sqlStatementParameters are the parameters every DBTX method running a statement takes.
	sqlStatementParameters = []emitkit.DBTXParameter{
		{Name: goast.IdentCtx, Type: sqlmodel.GoType{Composite: nil, Package: importContext, Name: "Context"}, Variadic: false},
		{Name: goast.IdentQuery, Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, Variadic: false},
		{Name: goast.IdentArgs, Type: sqlmodel.GoType{Composite: nil, Package: "", Name: goast.IdentAny}, Variadic: true},
	}

	// sqlQuerier describes the querier of a database/sql package: reader and writer
	// connections, a replica constructor, and transactions a prepared-statement cache can
	// wrap.
	sqlQuerier = emitkit.QuerierSpec{
		Transactions: &emitkit.TransactionSpec{
			Type:      sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "*Tx"},
			Parameter: goast.IdentTransaction,
			Begin:     "BeginTx",
			Source: emitkit.DBTXParameter{
				Name:     goast.IdentDB,
				Type:     sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "*DB"},
				Variadic: false,
			},
			BeginOptions:    true,
			EndTakesContext: false,
		},
		Connections: emitkit.Connections{Reader: goast.IdentReader, Writer: goast.IdentWriter},
		DBTX: []emitkit.DBTXMethod{
			{Name: "ExecContext", Parameters: sqlStatementParameters, Results: []sqlmodel.GoType{
				{Composite: nil, Package: importSQL, Name: "Result"},
				{Composite: nil, Package: "", Name: goast.IdentError},
			}},
			{Name: "QueryContext", Parameters: sqlStatementParameters, Results: []sqlmodel.GoType{
				{Composite: nil, Package: importSQL, Name: "*Rows"},
				{Composite: nil, Package: "", Name: goast.IdentError},
			}},
			{Name: "QueryRowContext", Parameters: sqlStatementParameters, Results: []sqlmodel.GoType{
				{Composite: nil, Package: importSQL, Name: "*Row"},
			}},
		},
		Replica:     true,
		PackageNote: "",
		WrapDBTX:    true,
	}
)

var (
	_ gogen.ParameterBinder = (*sqlDriver)(nil)

	_ gogen.PositionalBinder = (*sqlDriver)(nil)

	_ gogen.ColumnReader = (*sqlDriver)(nil)

	_ gogen.ColumnConverter = convertingReader{}

	_ gogen.OverrideReader = (*sqlDriver)(nil)

	_ gogen.HelperProvider = (*sqlDriver)(nil)

	_ gogen.BatchBuilder = (*sqlDriver)(nil)
)

// sqlDriver is what the database/sql emitter adds to one request's generation: how its
// driver binds parameters and reads columns, the helper files the package needs, and its
// batch methods.
type sqlDriver struct {
	// shapes collects the generation-time descriptors used by ClickHouse bindings.
	shapes chvalues.Shapes

	// profile records how the driver behind database/sql binds and scans values.
	profile *driverProfile

	// dialect describes the dialect the queries were analysed with.
	dialect *dialect.Facts

	// types maps the request's SQL types, and says which Go types an override chose.
	types sqlmodel.GoTypeResolver

	// vocabulary names the generated helpers of the request.
	vocabulary gogen.Vocabulary

	// prepared reports that the package gets the prepared-statement cache.
	prepared bool

	// wrapsDriverComposites is true when a query binds a parameter through the
	// driver-composite conversion, so the package needs the conversion's file.
	wrapsDriverComposites bool

	// wrapsInferredLists is true when a query binds a list parameter through the
	// list-inference wrapper, which the conversion's file holds beside the conversion.
	wrapsInferredLists bool

	// wrapsDriverValues is true when a query binds a parameter through the driver-value
	// wrapper, so the package needs the wrapper's file.
	wrapsDriverValues bool

	// wrapsDriverText is true when a query binds a parameter through the driver-text
	// wrapper, so the package needs the wrapper's file.
	wrapsDriverText bool

	// wrapsDriverJSON is true when a query binds a JSON parameter through the driver-JSON
	// wrapper, so the package needs the wrapper's file.
	wrapsDriverJSON bool

	// wrapsDriverBinary is true when a query binds a binary parameter through the
	// driver-binary wrapper, so the package needs the wrapper's file.
	wrapsDriverBinary bool

	// wrapsDriverDecimals is true when a query binds a decimal held by a pointer through the
	// driver-decimal wrapper, so the package needs the wrapper's file.
	wrapsDriverDecimals bool
}

// newSQLDriver works out what one request needs of its database/sql driver.
//
// Takes request (*emit.Request) which supplies the dialect, the queries and the
// vocabulary.
// Takes profile (*driverProfile) which is the profile of the driver the emitter generates
// for.
// Takes prepared (bool) which gives the package the prepared-statement cache.
//
// Returns *sqlDriver which binds, reads and batches for the request.
func newSQLDriver(request *emit.Request, profile *driverProfile, prepared bool) *sqlDriver {
	return &sqlDriver{
		vocabulary:            gogen.NewVocabulary(request),
		profile:               profile,
		dialect:               &request.Dialect,
		prepared:              prepared,
		wrapsDriverComposites: anyParameterBindsAsDriverComposite(profile, request.Queries, request.Types),
		wrapsInferredLists:    anyParameterBindsAsInferredList(profile, request.Queries),
		wrapsDriverValues:     anyParameterBindsThroughValuer(profile, request.Queries, request.Types),
		wrapsDriverText:       anyParameterBindsAsDriverText(profile, request.Queries, request.Types),
		wrapsDriverJSON:       anyParameterBindsAsJSONDocument(profile, &request.Dialect, request.Queries, request.Types),
		wrapsDriverBinary:     anyParameterBindsAsDriverBinary(profile, request.Queries, request.Types),
		wrapsDriverDecimals:   anyParameterBindsAsDriverDecimal(profile, request.Queries, request.Types),
		types:                 request.Types, shapes: chvalues.Shapes{}}
}

// ColumnReading selects direct, JSON, composite or text reading for a SQL type.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns gogen.ColumnReading which says how the column is read.
func (s *sqlDriver) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	return s.profile.reading(sqlType)
}

// ReadsOverride reports whether a column's overridden Go type needs a scan adapter.
//
// Predeclared types use database/sql conversion directly, except on a JSON column, whose
// document only Go's own string or bytes hold as database/sql reads it: a slice of
// strings, like any other Go type there, is decoded from the document. A column whose SQL
// type is not known, such as a function's result typed any, is read through the adapter
// too. Numbers, times and UUIDs retain their normal reading path.
//
// Takes goType (sqlmodel.GoType) which is the column's Go type.
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns bool which is true when the column is read through the adapter.
func (s *sqlDriver) ReadsOverride(goType sqlmodel.GoType, sqlType sqlmodel.SQLType) bool {
	switch {
	case s.decodesJSONOverride(goType, sqlType):
		return true
	case sqlType.Category == sqlmodel.TypeCategoryUnknown:
		return !gogen.PredeclaredGoType(goType)
	default:
		return slices.Contains(convertedOverrides, sqlType.Category) && !gogen.PredeclaredGoType(goType)
	}
}

// OverrideScan wraps a scan destination in the generated override scan adapter, as
// `sglScanOverride(destination)`, or, for a JSON column whose document the adapter
// decodes, `sglScanOverrideJSON(destination)`.
//
// Takes destination (ast.Expr) which is the address the column is read into.
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns ast.Expr which is the scan argument.
func (s *sqlDriver) OverrideScan(destination ast.Expr, sqlType sqlmodel.SQLType) ast.Expr {
	suffix := suffixScanOverride
	if s.decodesJSONOverride(sqlmodel.GoType{}, sqlType) {
		suffix = suffixScanOverrideJSON
	}
	return goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper(suffix)), destination)
}

// Bind returns the expression that binds a parameter.
//
// For a driver that binds named values and a dialect that binds by name, the access is
// wrapped in `clickhouse.Named("p_name", sglClickHouseValue(value, "<shape>"))` so the
// driver can substitute the value into the matching named placeholder. The helper writes
// the value as the text the server parses for the parameter's type, which the shape read
// from the parameter's SQL type describes.
//
// The bound name is the dialect's bound name for the parameter, not the declared name: a
// dialect whose engine rejects a parameter named after a reserved keyword gives a name
// prefix, and the matching placeholder is renamed with the same prefix, so the two always
// agree.
//
// A decimal held by a pointer whose type has a Decompose method, such as a nullable
// *apd.Decimal, which database/sql hands the driver as it is, is wrapped in the generated
// sglDriverDecimalValue on a driver that does not bind one itself, so a nil one binds
// NULL and any other the value its driver.Valuer writes. A driver that binds a JSON
// parameter only as text or bytes, without consulting its driver.Valuer, gets the access
// wrapped in the generated sglDriverValue, whose Value converts it as database/sql would;
// a binary parameter whose Go type an override chose is wrapped in the generated
// sglDriverBinary, which binds the bytes a value marshals itself to; a driver that binds
// a []byte as a binary value gets a text parameter whose Go type an override chose
// wrapped in the generated sglDriverText, which binds bytes as text; and a driver that
// binds a MAP only from a map type of its own, or a defined type as it is, gets a MAP,
// STRUCT or list-of-STRUCT parameter, or one whose Go type an override chose, converted
// by the generated sglDriverComposite; and a list whose elements the driver cannot build
// from the parameter's type is converted the same way and handed over by the generated
// sglDriverInferredList, to be bound by the types of its elements.
//
// Takes access (ast.Expr) which is the parameter access expression to wrap.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes imports (*goast.ImportTracker) which collects the imports the binding needs.
//
// Returns ast.Expr which is the wrapped access, or the input verbatim when the driver
// binds the value as it is.
func (s *sqlDriver) Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, imports *goast.ImportTracker) ast.Expr {
	if bindsNamedValues(s.profile, s.dialect.Placeholders) {
		return s.clickHouseNamed(goastutil.StrLit(s.dialect.Placeholders.BoundName(parameter.Name)), access, parameter, imports)
	}
	if bindsAsDriverDecimal(s.profile, parameter, s.types) {
		return driverDecimalAccess(s.vocabulary, access)
	}
	if bindsAsJSONDocument(parameter, s.types) {
		return driverJSONAccess(s.vocabulary, access)
	}
	if bindsAsDriverBinary(s.profile, parameter, s.types) {
		return driverBinaryAccess(s.vocabulary, access)
	}
	if bindsThroughValuer(s.profile, parameter) {
		return driverValueAccess(s.vocabulary, access)
	}
	if bindsAsDriverText(s.profile, parameter, s.types) {
		return driverTextAccess(s.vocabulary, access)
	}
	if bindsAsInferredList(s.profile, parameter) {
		return inferredListAccess(s.vocabulary, access)
	}
	if bindsAsDriverComposite(s.profile, parameter, s.types) {
		return driverCompositeAccess(s.vocabulary, access)
	}
	return access
}

// BindPositional binds runtime builder parameters in positional SQL. Drivers that format
// named values into SQL receive nullable pointer parameters through sglClickHouseNullable
// to bind NULL without panicking; other parameters keep their access expression.
//
// Takes access (ast.Expr) which is the parameter access expression.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which is the wrapped or unchanged access expression.
func (s *sqlDriver) BindPositional(access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	if !bindsNamedValues(s.profile, s.dialect.Placeholders) {
		return access
	}
	return chvalues.BindPositional(s.vocabulary, access, parameter, s.types)
}

// Helpers returns the binding, scanning, document and preparation helpers a package
// needs.
//
// Takes request (*emit.Request) which is the request being emitted.
//
// Returns []sqlmodel.GeneratedFile which holds the helper files.
// Returns error when a query cannot be rendered or a helper cannot be.
func (s *sqlDriver) Helpers(request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	emission := gogen.NewEmission(request, s.driver())
	readsComposites, err := emission.ConvertsAnyColumn()
	if err != nil {
		return nil, err
	}
	plainOverrides, documentOverrides, err := s.overrideReads(emission)
	if err != nil {
		return nil, err
	}
	reading := helperReading{
		composites:        readsComposites,
		overrides:         plainOverrides,
		documentOverrides: documentOverrides,
		documents:         s.readsDocuments(emission),
	}
	renderers := s.helperRenderers(request, reading)
	files := make([]sqlmodel.GeneratedFile, 0, len(renderers))
	for _, render := range renderers {
		file, err := render()
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// Batch returns a batch method that accepts []Params and executes chunked multi-row
// INSERTs, repeating the query's VALUES row once per row.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the batch method declaration.
// Returns error when the query's VALUES row cannot be repeated.
func (s *sqlDriver) Batch(query *gogen.Query) (ast.Decl, error) {
	return s.batchMethod(query)
}

// RepeatsRow reports that a batch method writes the query's VALUES row once per row.
//
// Returns bool which is always true.
func (*sqlDriver) RepeatsRow(_ *gogen.Query) bool {
	return true
}

// CopyStatement reports that a copyfrom method, the same multi-row INSERT as a batch
// method, writes the query's VALUES row once per row.
//
// Returns gogen.CopyStatement which is always gogen.CopyStatementRepeatsRow.
func (*sqlDriver) CopyStatement(_ *gogen.Query) gogen.CopyStatement {
	return gogen.CopyStatementRepeatsRow
}

// CopyFrom returns the same multi-row INSERT method as Batch, since there is no COPY
// protocol in standard SQL.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the copyfrom method declaration.
// Returns error when the query's VALUES row cannot be repeated.
func (s *sqlDriver) CopyFrom(query *gogen.Query) (ast.Decl, error) {
	return s.batchMethod(query)
}

// decodesJSONOverride reports whether a JSON column is decoded into its overridden Go
// type. Drivers returning document text decode into types other than string or bytes;
// drivers returning their own document values use the override scan adapter.
//
// Takes goType (sqlmodel.GoType) which is the column's Go type.
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns bool which is true when the document is decoded.
func (s *sqlDriver) decodesJSONOverride(goType sqlmodel.GoType, sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryJSON && !s.profile.readsDocuments && !gogen.HoldsTextOrBytes(goType)
}

// overrideReads reports how the generated code reads the columns whose Go type an
// override chose, so the package holds the adapters it calls and no other: a column the
// query's SQL wraps as JSON, such as an array, reads through the JSON scan adapter
// instead.
//
// Takes emission (*gogen.Emission) which holds the request.
//
// Returns plain which is true when a column is read through the override scan adapter.
// Returns documents which is true when a JSON column's document is decoded.
// Returns err (error) when a query cannot be rendered.
func (s *sqlDriver) overrideReads(emission *gogen.Emission) (plain, documents bool, err error) {
	for _, analysed := range emission.Request.Queries {
		scanned, err := emission.OverrideScannedColumns(analysed)
		if err != nil {
			return false, false, err
		}
		for index := range analysed.OutputColumns {
			if !scanned[index] {
				continue
			}
			column := &analysed.OutputColumns[index]
			goType := gogen.ChosenGoType(emission.Request.Types, column.GoTypeOverride, column.SQLType, column.Nullable)
			if s.decodesJSONOverride(goType, column.SQLType) {
				documents = true
			} else {
				plain = true
			}
		}
	}
	return plain, documents, nil
}

// reader returns the driver's column reader: one with the driver-composite scan adapter
// when the profile reads some columns through it, its own forms or the documents it
// returns inside a composite, so only such a driver has the columns its query cannot cast
// to text read through the adapter.
//
// Returns gogen.ColumnReader which reads the driver's columns.
func (s *sqlDriver) reader() gogen.ColumnReader {
	if len(s.profile.readsConverted) > 0 || s.profile.readsDocuments {
		return convertingReader{sqlDriver: s}
	}
	return s
}

// clickHouseNamed builds the named value of a parameter: `clickhouse.Named(<name>,
// sglClickHouseValue(<access>, "<shape>"))`, referring to the clickhouse-go package by
// the qualifier the file imports it under.
//
// Takes name (ast.Expr) which evaluates to the bound name.
// Takes access (ast.Expr) which reads the parameter value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes imports (*goast.ImportTracker) which collects the imports of the file the call is
// written in.
//
// Returns ast.Expr which is the Named call.
func (s *sqlDriver) clickHouseNamed(name ast.Expr, access ast.Expr, parameter *sqlmodel.QueryParameter, imports *goast.ImportTracker) ast.Expr {
	if gogen.EncodesJSONDocument(s.types, parameter) {
		access = chvalues.Document(s.vocabulary, access)
	}
	named := imports.AddType(sqlmodel.GoType{Composite: nil, Package: driverClickHouse, Name: "Named"})
	return goastutil.CallExpr(named, name, s.shapes.Bind(s.vocabulary, access, parameter))
}

// readsDocuments reports whether the generated code reads a JSON document the driver
// returns as its own through the driver-composite or the override scan adapter, so the
// package needs clickhouse_documents.go: a column holding one read through either, on a
// driver whose profile reads documents.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns bool which is true when a column holding a document is read so.
func (s *sqlDriver) readsDocuments(emission *gogen.Emission) bool {
	if !s.profile.readsDocuments {
		return false
	}
	for _, query := range emission.Request.Queries {
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			if chvalues.HoldsJSON(&column.SQLType) && (emission.ConvertsColumn(column, false) || emission.ReadsOverride(column)) {
				return true
			}
		}
	}
	return false
}

// helperRenderers lists the renderers of the helper files a request's package needs, in
// the order the files are written.
//
// Takes request (*emit.Request) which is the request being emitted.
// Takes reading (helperReading) which says which columns are read through the helpers.
//
// Returns []func() (sqlmodel.GeneratedFile, error) which holds the renderers.
func (s *sqlDriver) helperRenderers(request *emit.Request, reading helperReading) []func() (sqlmodel.GeneratedFile, error) {
	packageName := request.Package
	var renderers []func() (sqlmodel.GeneratedFile, error)
	switch {
	case bindsNamedValues(s.profile, s.dialect.Placeholders):
		documents := chvalues.BindsDocuments(request.Queries, s.types)
		parts := chvalues.ValueParts{Shapes: &s.shapes, Documents: documents, Nullables: chvalues.BindsNullablesPositionally(request.Queries, s.types),
			Durations: chvalues.BindsDurations(request.Queries)}
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) { return chvalues.Values(s.vocabulary, packageName, parts) })
	case s.wrapsDriverValues || s.wrapsDriverText || s.wrapsDriverJSON || s.wrapsDriverBinary || s.wrapsDriverDecimals:
		wrappers := driverWrappers{
			values: s.wrapsDriverValues, text: s.wrapsDriverText, documents: s.wrapsDriverJSON, binary: s.wrapsDriverBinary,
			decimals: s.wrapsDriverDecimals,
		}
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) {
			return renderDriverValues(s.vocabulary, packageName, wrappers)
		})
	}
	if s.wrapsDriverComposites || s.wrapsInferredLists || reading.composites || reading.overrides || reading.documentOverrides {
		composites := driverComposites{
			binds:             s.wrapsDriverComposites || s.wrapsInferredLists,
			lists:             s.wrapsInferredLists,
			scans:             reading.composites,
			overrides:         reading.overrides,
			documentOverrides: reading.documentOverrides,
			ownTypes:          len(s.profile.readsConverted) > 0,
			documents:         reading.documents,
		}
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) {
			return renderDriverComposites(s.vocabulary, packageName, composites)
		})
	}
	if reading.composites || reading.overrides || reading.documentOverrides {
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) {
			return valuereading.Render(s.vocabulary, packageName)
		})
	}
	if reading.documents {
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) {
			return chvalues.Documents(s.vocabulary, packageName, chvalues.JSONType(request.Types))
		})
	}
	if s.prepared {
		renderers = append(renderers, func() (sqlmodel.GeneratedFile, error) {
			return renderPrepared(s.vocabulary, request)
		})
	}
	return renderers
}

// driver returns the gogen.Driver of the request.
//
// Returns *gogen.Driver which describes database/sql and carries this driver's behaviour:
// a slice's list is expanded to one placeholder per element, except in a form that binds
// by name, where the slice is bound whole, as one value; and an enum is read and bound
// through Go's own strings where the profile says.
func (s *sqlDriver) driver() *gogen.Driver {
	return &gogen.Driver{
		Binder:     s,
		Reader:     s.reader(),
		Helpers:    s,
		Batch:      s,
		Querier:    sqlQuerier,
		Connection: sqlConnection,
		Lists:      sqlLists,
		Enums:      s.profile.enums,
	}
}

// convertingReader is the column reader of a driver whose profile reads some columns
// through the driver-composite scan adapter: the driver's reader, and the adapter that
// converts the driver's own value of such a column.
type convertingReader struct {
	*sqlDriver
}

// ConvertingScan wraps a scan destination in the generated driver-composite scan adapter,
// as `sglScanDriverComposite(destination)`, which converts the driver's own value of the
// column into the destination's Go type.
//
// Takes destination (ast.Expr) which is the address the column is read into.
//
// Returns ast.Expr which is the scan argument.
func (reader convertingReader) ConvertingScan(destination ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(reader.vocabulary.Helper(suffixScanDriverComposite)), destination)
}

// helperReading says which columns the generated code reads through the helpers.
type helperReading struct {
	// composites reports that a column is read through the driver-composite scan adapter.
	composites bool

	// overrides reports that a column is read through the override scan adapter.
	overrides bool

	// documentOverrides reports that a JSON column's document is decoded into the Go type an
	// override chose.
	documentOverrides bool

	// documents reports that a JSON document the driver returns as its own is read through
	// the composite or override scan adapter.
	documents bool
}
