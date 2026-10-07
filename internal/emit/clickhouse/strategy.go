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

package clickhouse

import (
	"go/ast"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/multirow"
	"seagull.sh/seagull/internal/emit/internal/valuereading"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// nativeDriverName names the native driver in the problems it has with a request.
	nativeDriverName = "the native ClickHouse driver"

	// nativeLists is how the generated code writes a slice's list for the native driver: one
	// placeholder per element, except in ClickHouse's own form, which binds by name.
	nativeLists = emitkit.ListExpand
)

var (
	// nativeConnection says how the generated code calls a native ClickHouse connection: its
	// Exec returns only an error, and its count() comes back as a UInt64.
	nativeConnection = gogen.Connection{
		ExecResult:         nil,
		Query:              "Query",
		QueryRow:           "QueryRow",
		Exec:               "Exec",
		Reader:             goast.IdentDB,
		Writer:             goast.IdentDB,
		CountScan:          "uint64",
		NoRows:             sqlmodel.GoType{Composite: nil, Package: "database/sql", Name: "ErrNoRows"},
		RowsAffectedErrors: false,
	}

	// contextParameter is the context every DBTX method takes first.
	contextParameter = emitkit.DBTXParameter{
		Name:     goast.IdentCtx,
		Type:     sqlmodel.GoType{Composite: nil, Package: "context", Name: "Context"},
		Variadic: false,
	}

	// queryParameter is the SQL every DBTX method takes after the context.
	queryParameter = emitkit.DBTXParameter{
		Name:     goast.IdentQuery,
		Type:     sqlmodel.GoType{Composite: nil, Package: "", Name: "string"},
		Variadic: false,
	}

	// statementParameters are the parameters every DBTX method running a statement takes.
	statementParameters = []emitkit.DBTXParameter{
		contextParameter,
		queryParameter,
		{Name: goast.IdentArgs, Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "any"}, Variadic: true},
	}

	// nativeQuerier describes the querier of a native ClickHouse package: one connection and
	// no transactions.
	nativeQuerier = emitkit.QuerierSpec{
		Transactions: nil,
		Connections:  emitkit.Connections{Reader: goast.IdentDB, Writer: goast.IdentDB},
		DBTX: []emitkit.DBTXMethod{
			{Name: "Query", Parameters: statementParameters, Results: []sqlmodel.GoType{driverType("Rows"), errorType}},
			{Name: "QueryRow", Parameters: statementParameters, Results: []sqlmodel.GoType{driverType("Row")}},
			{Name: "Exec", Parameters: statementParameters, Results: []sqlmodel.GoType{errorType}},
			{Name: "PrepareBatch", Parameters: []emitkit.DBTXParameter{
				contextParameter,
				queryParameter,
				{Name: "opts", Type: driverType("PrepareBatchOption"), Variadic: true},
			}, Results: []sqlmodel.GoType{driverType("Batch"), errorType}},
		},
		Replica:     false,
		PackageNote: "",
		WrapDBTX:    false,
	}

	// errorType is the built-in error type.
	errorType = sqlmodel.GoType{Composite: nil, Package: "", Name: "error"}
)

var (
	// nativeEnums describes where the native driver requires enum conversion to plain
	// strings.
	//
	// Enum parameters bind without conversion. Scanning may need plain strings; unsupported
	// nested enum Maps are refused.
	nativeEnums = gogen.EnumStrings{Scan: gogen.EnumValue | gogen.EnumMap | gogen.EnumNestedInMap, Bind: 0}

	// nativeBatchEnums says how a columnar batch appends the enum types a package declares:
	// its columns take a value, a pointer, a slice, a slice of pointers or a map's values
	// only as Go's own strings, so an enum held deeper cannot be appended.
	nativeBatchEnums = gogen.EnumStrings{
		Scan: 0,
		Bind: gogen.EnumValue | gogen.EnumSlice | gogen.EnumMap | gogen.EnumNested | gogen.EnumNestedInMap,
	}
)

// nativeDriver is what the native ClickHouse emitter adds to one request's generation:
// how a dialect that binds by name binds parameters, the helpers that binding needs, and
// the batch methods.
type nativeDriver struct {
	// shapes collects the generation-time descriptors used by parameter bindings.
	shapes chvalues.Shapes

	// dialect describes the dialect the queries were analysed with.
	dialect *dialect.Facts

	// types maps the request's SQL types, and says which Go types an override chose.
	types sqlmodel.GoTypeResolver

	// vocabulary names the generated helpers of the request.
	vocabulary gogen.Vocabulary

	// bindsParameters is true when a query binds a parameter through clickhouse.Named, so
	// the package needs the generated value helper.
	bindsParameters bool

	// bindsDocuments is true when a query binds a JSON parameter as the document its value
	// encodes to, so the value helper's file holds sglClickHouseEncoded.
	bindsDocuments bool

	// bindsNullablesPositionally is true when a runtime builder binds a nullable parameter
	// held through a pointer through sglClickHouseNullable, so the value helper's file holds
	// it.
	bindsNullablesPositionally bool
}

var (
	_ gogen.ParameterBinder = (*nativeDriver)(nil)

	_ gogen.PositionalBinder = (*nativeDriver)(nil)

	_ gogen.HelperProvider = (*nativeDriver)(nil)

	_ gogen.BatchBuilder = (*nativeDriver)(nil)
)

// newNativeDriver works out what one request needs of the native driver.
//
// Takes request (*emit.Request) which supplies the dialect, the queries and the
// vocabulary.
//
// Returns *nativeDriver which binds and batches for the request.
func newNativeDriver(request *emit.Request) *nativeDriver {
	bindsByName := request.Dialect.Placeholders.BindsByName()
	return &nativeDriver{
		vocabulary:                 gogen.NewVocabulary(request),
		dialect:                    &request.Dialect,
		types:                      request.Types,
		bindsParameters:            bindsByName && bindsParameters(request.Queries),
		bindsDocuments:             bindsByName && chvalues.BindsDocuments(request.Queries, request.Types),
		bindsNullablesPositionally: bindsByName && chvalues.BindsNullablesPositionally(request.Queries, request.Types), shapes: chvalues.Shapes{}}
}

// Bind wraps named values for ClickHouse binding and preserves positional accesses.
//
// Named values are formatted according to the parameter's SQL shape. A JSON parameter
// whose Go type an override chose is bound as the document its value encodes to, through
// sglClickHouseEncoded.
//
// The bound name is the dialect's bound name for the parameter, which carries the name
// prefix that keeps a parameter named after a reserved keyword away from the server's
// parser, and matches the placeholder the SQL constant writes.
//
// Takes access (ast.Expr) which is the parameter access expression.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes imports (*goast.ImportTracker) which collects the imports the binding needs.
//
// Returns ast.Expr which is the wrapped or unchanged access expression.
func (n *nativeDriver) Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, imports *goast.ImportTracker) ast.Expr {
	if !n.dialect.Placeholders.BindsByName() {
		return access
	}
	if gogen.EncodesJSONDocument(n.types, parameter) {
		access = chvalues.Document(n.vocabulary, access)
	}
	return goastutil.CallExpr(
		imports.AddType(sqlmodel.GoType{Composite: nil, Package: importClickHouse, Name: "Named"}),
		goastutil.StrLit(n.dialect.Placeholders.BoundName(parameter.Name)),
		n.shapes.Bind(n.vocabulary, access, parameter),
	)
}

// BindPositional binds a parameter of a runtime builder, which the driver formats into
// the statement's text itself: a nullable parameter held through a pointer through
// sglClickHouseNullable, so a nil one binds NULL rather than panicking in the driver, and
// any other as it is.
//
// Takes access (ast.Expr) which is the parameter access expression.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which is the wrapped or unchanged access expression.
func (n *nativeDriver) BindPositional(access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	return chvalues.BindPositional(n.vocabulary, access, parameter, n.types)
}

// Helpers returns the value, scan, document and append helpers needed by a package.
//
// Takes request (*emit.Request) which is the request being emitted.
//
// Returns []sqlmodel.GeneratedFile which holds the helper files, or none.
// Returns error when a query or a helper cannot be rendered.
func (n *nativeDriver) Helpers(request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	var files []sqlmodel.GeneratedFile
	appends := appendsThroughConversion(request.Queries, request.Types)
	if n.bindsParameters || appends {
		values, err := chvalues.Values(n.vocabulary, request.Package,
			chvalues.ValueParts{Shapes: &n.shapes, Documents: n.bindsDocuments, Nullables: n.bindsNullablesPositionally,
				Durations: chvalues.BindsDurations(request.Queries)})
		if err != nil {
			return nil, err
		}
		files = append(files, values)
	}
	emission := gogen.NewEmission(request, n.driver())
	converts, err := emission.ConvertsAnyColumn()
	if err != nil {
		return nil, err
	}
	parts := documentParts(emission)
	if converts || emission.ReadsAnyOverride() {
		reading, err := n.readingFiles(request.Package, parts)
		if err != nil {
			return nil, err
		}
		files = append(files, reading...)
	}
	if parts.documents {
		documents, err := chvalues.Documents(n.vocabulary, request.Package, chvalues.JSONType(request.Types))
		if err != nil {
			return nil, err
		}
		files = append(files, documents)
	}
	if appends {
		appending, err := chvalues.Appending(n.vocabulary, request.Package)
		if err != nil {
			return nil, err
		}
		files = append(files, appending)
	}
	return files, nil
}

// Batch builds a native columnar or multi-row INSERT batch method.
//
// A row of lone placeholders uses a native batch. Expressions and literals use repeated
// VALUES rows with distinct bindings.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the query's VALUES row cannot be repeated.
func (n *nativeDriver) Batch(query *gogen.Query) (ast.Decl, error) {
	if _, columns := emitkit.RowColumns(query.Analysed); columns {
		return n.CopyFrom(query)
	}
	spec := &multirow.Spec{
		Bind: func(access ast.Expr, parameter *sqlmodel.QueryParameter, name ast.Expr) ast.Expr {
			if name == nil {
				return n.Bind(access, parameter, query.Imports)
			}
			if gogen.EncodesJSONDocument(n.types, parameter) {
				access = chvalues.Document(n.vocabulary, access)
			}
			named := query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importClickHouse, Name: "Named"})
			return goastutil.CallExpr(named, name, n.shapes.Bind(n.vocabulary, access, parameter))
		},
		Vocabulary: n.vocabulary,
		Connection: nativeConnection,
		Limit:      batchLimit(n.dialect),
		PerRow:     false,
	}
	method, err := multirow.Method(query, spec)
	if err != nil {
		return nil, err
	}
	return method, nil
}

// RepeatsRow reports whether a batch method writes the query's VALUES row once per row,
// which it does unless every item of the row is a placeholder alone, whose values it
// appends to a prepared native batch instead.
//
// Takes query (*gogen.Query) which is the batch query being generated.
//
// Returns bool which is true when the method repeats the row.
func (*nativeDriver) RepeatsRow(query *gogen.Query) bool {
	_, columns := emitkit.RowColumns(query.Analysed)
	return !columns
}

// CopyStatement reports that a copyfrom method runs the query's SQL as it is, preparing a
// native batch from it.
//
// Returns gogen.CopyStatement which is always gogen.CopyStatementAsIs.
func (*nativeDriver) CopyStatement(_ *gogen.Query) gogen.CopyStatement {
	return gogen.CopyStatementAsIs
}

// CopyFrom returns a copyfrom method that appends each row to a prepared native batch,
// its values in the order of the items of the query's VALUES row.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the query's VALUES row is not one placeholder per column.
func (n *nativeDriver) CopyFrom(query *gogen.Query) (ast.Decl, error) {
	method, err := buildClickHouseBatchMethod(query, n.vocabulary, &n.shapes)
	if err != nil {
		return nil, err
	}
	return method, nil
}

// readingFiles renders the files a package reading columns through the converting scan
// adapter needs: the adapter, with the parts the package uses, and the shared readers it
// reads lists, maps and scalars with.
//
// Takes packageName (string) which is the generated package's name.
// Takes parts (compositeParts) which says which parts of the adapter the package uses.
//
// Returns []sqlmodel.GeneratedFile which holds the two files.
// Returns error when either cannot be rendered.
func (n *nativeDriver) readingFiles(packageName string, parts compositeParts) ([]sqlmodel.GeneratedFile, error) {
	composites, err := renderComposites(n.vocabulary, packageName, parts)
	if err != nil {
		return nil, err
	}
	reading, err := valuereading.Render(n.vocabulary, packageName)
	if err != nil {
		return nil, err
	}
	return []sqlmodel.GeneratedFile{composites, reading}, nil
}

// driver returns the gogen.Driver of the request.
//
// Returns *gogen.Driver which describes the native driver and carries this driver's
// behaviour: a slice's list is expanded to one placeholder per element, except in a form
// that binds by name, ClickHouse's own, where the slice is bound whole, as one array; an
// enum value is scanned through Go's own strings; and a column holding a Map of named
// tuples is read through the converting scan adapter.
func (n *nativeDriver) driver() *gogen.Driver {
	return &gogen.Driver{
		Binder:     n,
		Reader:     n,
		Helpers:    n,
		Batch:      n,
		Querier:    nativeQuerier,
		Connection: nativeConnection,
		Lists:      nativeLists,
		Enums:      nativeEnums,
	}
}

// driverType returns a type of the clickhouse-go driver package.
//
// Takes name (string) which is the type's name.
//
// Returns sqlmodel.GoType which is the type.
func driverType(name string) sqlmodel.GoType {
	return sqlmodel.GoType{Composite: nil, Package: importDriver, Name: name}
}

// batchLimit returns the most named values one multi-row batch statement binds: the
// dialect's limit, lowered to the number the server handles quickly.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns int which is the limit.
func batchLimit(info *dialect.Facts) int {
	limit := info.MaxParameters
	if limit <= 0 {
		limit = goast.DefaultMaxBindVariablesFallback
	}
	return min(limit, chvalues.BatchParameters)
}
