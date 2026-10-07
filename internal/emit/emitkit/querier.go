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

package emitkit

import (
	"fmt"
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// querierFile is the name of the generated querier file.
	querierFile = "querier.go"

	// identDBTX names the interface every connection the queries run on satisfies.
	identDBTX = "DBTX"

	// identWrapperInterface names the interface a DBTX implements when it wraps a
	// transaction in a view of its own, such as a cache of prepared statements.
	identWrapperInterface = "dbtxWrapper"

	// identWrapMethod names the method of the wrapper interface.
	identWrapMethod = "WrapDBTX"

	// identWrapperField names the Queries field holding the wrapper, and the local the
	// constructors bind it to.
	identWrapperField = "wrapper"

	// identWrapHelper names the method that applies the wrapper, if any.
	identWrapHelper = "wrapDBTX"

	// identInner names the parameter of the wrapper interface's method.
	identInner = "inner"

	// identWrapped names the wrapped connection before it is asserted back to a DBTX.
	identWrapped = "wrapped"

	// identOK names the comma-ok result of the assertion.
	identOK = "ok"

	// identAny is the parameter and result type of the wrapper interface's method.
	identAny = "any"

	// identFunction names RunInTx's function parameter.
	identFunction = "fn"

	// identNew names the constructor of a querier.
	identNew = "New"

	// identNewWithReplica names the constructor of a querier whose read-only queries run on
	// a replica.
	identNewWithReplica = "NewWithReplica"

	// identWithTx names the method that returns a querier running on a transaction.
	identWithTx = "WithTx"

	// identRunInTx names the method that runs a function inside a transaction.
	identRunInTx = "RunInTx"

	// identBlank is the blank identifier.
	identBlank = "_"
)

// QuerierSpec describes the querier an emitter generates: the DBTX interface its
// connections satisfy, the connections the Queries struct holds and the transaction
// helpers.
type QuerierSpec struct {
	// Transactions describes the transaction helpers, or is nil for a driver without
	// transactions.
	Transactions *TransactionSpec

	// Connections names the Queries fields the generated methods run on.
	Connections Connections

	// PackageNote completes the package doc comment the querier file carries, after "Package
	// <name>", with what the package's reader must know of its driver, such as a setting the
	// driver's connection must have. Empty writes no package doc comment.
	PackageNote string

	// DBTX lists the methods of the DBTX interface, in order.
	DBTX []DBTXMethod

	// Replica reports that the querier has a constructor taking a separate connection for
	// read-only queries; it needs separate reader and writer fields.
	Replica bool

	// WrapDBTX reports that a connection may wrap a transaction in a DBTX of its own, such
	// as a cache of prepared statements, which the querier then runs the transaction's
	// queries on.
	WrapDBTX bool
}

// DBTXMethod is one method of the DBTX interface.
type DBTXMethod struct {
	// Name is the method's name.
	Name string

	// Parameters lists the method's parameters, in order.
	Parameters []DBTXParameter

	// Results lists the types of the method's results, in order.
	Results []sqlmodel.GoType
}

// DBTXParameter is one named parameter of a generated signature.
type DBTXParameter struct {
	// Name is the parameter's name.
	Name string

	// Type is the parameter's type; a variadic parameter's element type.
	Type sqlmodel.GoType

	// Variadic reports that the parameter takes any number of values.
	Variadic bool
}

// Connections names the Queries fields the generated methods run on: one for read-only
// queries and one for the rest, which are the same field for a querier with one
// connection.
type Connections struct {
	// Reader is the field read-only queries run on.
	Reader string

	// Writer is the field every other query runs on.
	Writer string
}

// TransactionSpec describes the transaction helpers of a querier: WithTx, which returns a
// querier running on a transaction, and RunInTx, which runs a function inside one.
type TransactionSpec struct {
	// Type is the type of a transaction, such as *sql.Tx.
	Type sqlmodel.GoType

	// Parameter names WithTx's transaction parameter.
	Parameter string

	// Begin is the connection method that begins a transaction.
	Begin string

	// Source is RunInTx's parameter for the connection that begins the transaction.
	Source DBTXParameter

	// BeginOptions reports that Begin takes transaction options after the context, which
	// RunInTx passes as nil.
	BeginOptions bool

	// EndTakesContext reports that the transaction's Rollback and Commit take the context.
	EndTakesContext bool
}

// Querier generates the querier file of a package, querier.go: the DBTX interface, the
// Queries struct with its constructors, and the transaction helpers, under a package doc
// comment where the spec has a PackageNote. The file starts with no header; the caller
// writes the style's header before it.
//
// The names it declares are fixed, so the emitter's emit.Reservation lists them: the
// declarations DBTX, Queries and New, NewWithReplica for a spec with a replica and
// dbtxWrapper for one that wraps transactions; the Queries fields the Connections name,
// and wrapper for one that wraps transactions; the Queries methods WithTx and RunInTx,
// and wrapDBTX for one that wraps transactions; and the file querier.go.
//
// Takes packageName (string) which is the generated package's name.
// Takes naming (emit.Naming) which is the package's naming plan. The querier declares
// only the fixed names above and refers to packages by the names their import paths
// imply, which the plan keeps every planned name clear of, so it reads nothing from the
// plan today; it takes it so the querier can follow the plan without a change of
// signature.
// Takes spec (QuerierSpec) which describes the querier.
//
// Returns sqlmodel.GeneratedFile which is the querier file.
// Returns error when the spec is inconsistent, such as a replica sharing one connection
// field, or the file does not format.
func Querier(packageName string, naming emit.Naming, spec QuerierSpec) (sqlmodel.GeneratedFile, error) {
	_ = naming
	if spec.Replica && spec.Connections.Reader == spec.Connections.Writer {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("querier: a replica needs separate reader and writer fields, not %q", spec.Connections.Reader)
	}
	tracker := goast.NewImportTracker()
	tracker.AddImport(goast.IdentContext)
	packageDoc := ""
	if spec.PackageNote != "" {
		packageDoc = "Package " + packageName + " " + spec.PackageNote
	}
	content, err := goast.FormatDocumentedFile(packageName, packageDoc, tracker, querierDeclarations(&spec, tracker))
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("formatting querier file: %w", err)
	}
	return sqlmodel.GeneratedFile{Name: querierFile, Content: content}, nil
}

// querierDeclarations returns every declaration of the querier file, in order.
//
// Takes spec (*QuerierSpec) which describes the querier.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns []ast.Decl which holds the declarations.
func querierDeclarations(spec *QuerierSpec, tracker *goast.ImportTracker) []ast.Decl {
	declarations := []ast.Decl{dbtxInterface(spec.DBTX, tracker)}
	if spec.WrapDBTX {
		declarations = append(declarations, wrapperInterface())
	}
	declarations = append(declarations, queriesStruct(spec), newFunction(spec))
	if spec.Replica {
		declarations = append(declarations, newWithReplicaFunction(spec))
	}
	if spec.Transactions != nil {
		declarations = append(declarations, transactionDeclarations(spec, tracker)...)
	}
	return declarations
}

// transactionDeclarations returns WithTx, wrapDBTX when the spec wraps transactions, and
// RunInTx.
//
// Takes spec (*QuerierSpec) which describes the querier; its Transactions is set.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns []ast.Decl which holds the declarations.
func transactionDeclarations(spec *QuerierSpec, tracker *goast.ImportTracker) []ast.Decl {
	declarations := []ast.Decl{withTxMethod(spec, tracker)}
	if spec.WrapDBTX {
		declarations = append(declarations, wrapDBTXMethod())
	}
	return append(declarations, runInTxMethod(spec.Transactions, tracker))
}

// dbtxInterface returns the DBTX interface declaration.
//
// Takes methods ([]DBTXMethod) which are its methods.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns *ast.GenDecl which is the declaration.
func dbtxInterface(methods []DBTXMethod, tracker *goast.ImportTracker) *ast.GenDecl {
	fields := make([]*ast.Field, len(methods))
	for index := range methods {
		fields[index] = &ast.Field{
			Names: []*ast.Ident{goastutil.CachedIdent(methods[index].Name)},
			Type: &ast.FuncType{
				Params:  parameterList(methods[index].Parameters, tracker),
				Results: resultList(methods[index].Results, tracker),
			},
		}
	}
	return goastutil.GenDeclType(identDBTX, &ast.InterfaceType{Methods: &ast.FieldList{List: fields}})
}

// parameterList returns a signature's parameter list.
//
// Takes parameters ([]DBTXParameter) which are the parameters.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns *ast.FieldList which is the list.
func parameterList(parameters []DBTXParameter, tracker *goast.ImportTracker) *ast.FieldList {
	fields := make([]*ast.Field, len(parameters))
	for index := range parameters {
		fields[index] = parameterField(&parameters[index], tracker)
	}
	return &ast.FieldList{List: fields}
}

// parameterField returns one parameter of a signature.
//
// Takes parameter (*DBTXParameter) which is the parameter.
// Takes tracker (*goast.ImportTracker) which collects the imports its type needs.
//
// Returns *ast.Field which is the parameter.
func parameterField(parameter *DBTXParameter, tracker *goast.ImportTracker) *ast.Field {
	parameterType := tracker.AddType(parameter.Type)
	if parameter.Variadic {
		parameterType = &ast.Ellipsis{Elt: parameterType}
	}
	return &ast.Field{Names: []*ast.Ident{goastutil.CachedIdent(parameter.Name)}, Type: parameterType}
}

// resultList returns a signature's unnamed result list.
//
// Takes results ([]sqlmodel.GoType) which are the results' types.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns *ast.FieldList which is the list.
func resultList(results []sqlmodel.GoType, tracker *goast.ImportTracker) *ast.FieldList {
	fields := make([]*ast.Field, len(results))
	for index := range results {
		fields[index] = goastutil.Field("", tracker.AddType(results[index]))
	}
	return goastutil.FieldList(fields...)
}

// wrapperInterface returns the dbtxWrapper interface declaration.
//
// Returns *ast.GenDecl which is the declaration.
func wrapperInterface() *ast.GenDecl {
	return goastutil.GenDeclType(identWrapperInterface, &ast.InterfaceType{
		Methods: &ast.FieldList{
			List: []*ast.Field{{
				Names: []*ast.Ident{goastutil.CachedIdent(identWrapMethod)},
				Type: goastutil.FuncType(
					goastutil.FieldList(goastutil.Field(identInner, goastutil.CachedIdent(identAny))),
					goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(identAny))),
				),
			}},
		},
	})
}

// queriesStruct returns the Queries struct declaration: one field per connection, and the
// wrapper when the spec wraps transactions.
//
// Takes spec (*QuerierSpec) which describes the querier.
//
// Returns *ast.GenDecl which is the declaration.
func queriesStruct(spec *QuerierSpec) *ast.GenDecl {
	fields := []*ast.Field{goastutil.Field(spec.Connections.Reader, goastutil.CachedIdent(identDBTX))}
	if spec.Connections.Writer != spec.Connections.Reader {
		fields = append(fields, goastutil.Field(spec.Connections.Writer, goastutil.CachedIdent(identDBTX)))
	}
	if spec.WrapDBTX {
		fields = append(fields, goastutil.Field(identWrapperField, goastutil.CachedIdent(identWrapperInterface)))
	}
	return goastutil.GenDeclType(goast.IdentQueries, goastutil.StructType(fields...))
}

// queriesLiteral returns &Queries{...} with every connection field set to its value, and
// the wrapper field to wrapper when the spec wraps transactions.
//
// Takes spec (*QuerierSpec) which describes the querier.
// Takes reader (ast.Expr) which is the reader connection's value.
// Takes writer (ast.Expr) which is the writer connection's value, unused for a querier
// with one connection.
// Takes wrapper (ast.Expr) which is the wrapper's value.
//
// Returns ast.Expr which is the literal's address.
func queriesLiteral(spec *QuerierSpec, reader, writer, wrapper ast.Expr) ast.Expr {
	elements := []ast.Expr{goastutil.KeyValueIdent(spec.Connections.Reader, reader)}
	if spec.Connections.Writer != spec.Connections.Reader {
		elements = append(elements, goastutil.KeyValueIdent(spec.Connections.Writer, writer))
	}
	if spec.WrapDBTX {
		elements = append(elements, goastutil.KeyValueExpr(goastutil.CachedIdent(identWrapperField), wrapper))
	}
	return goastutil.AddressExpr(goastutil.CompositeLit(goastutil.CachedIdent(goast.IdentQueries), elements...))
}

// queriesPointer returns the *Queries type.
//
// Returns ast.Expr which is the type.
func queriesPointer() ast.Expr {
	return goastutil.StarExpr(goastutil.CachedIdent(goast.IdentQueries))
}

// queriesReceiver returns the receiver of a method on *Queries.
//
// Returns *ast.FieldList which is the receiver.
func queriesReceiver() *ast.FieldList {
	return goastutil.FieldList(goastutil.Field(goast.IdentQueriesReceiver, queriesPointer()))
}

// newFunction returns the New constructor, which runs every query on one connection and,
// when the spec wraps transactions, takes the connection's wrapper.
//
// Takes spec (*QuerierSpec) which describes the querier.
//
// Returns *ast.FuncDecl which is the declaration.
func newFunction(spec *QuerierSpec) *ast.FuncDecl {
	connection := goastutil.CachedIdent(goast.IdentDB)
	var statements []ast.Stmt
	if spec.WrapDBTX {
		statements = append(statements, resolveWrapper(goast.IdentDB))
	}
	statements = append(statements, goastutil.ReturnStmt(
		queriesLiteral(spec, connection, connection, goastutil.CachedIdent(identWrapperField)),
	))
	return goastutil.FuncDecl(
		identNew,
		goastutil.FieldList(goastutil.Field(goast.IdentDB, goastutil.CachedIdent(identDBTX))),
		goastutil.FieldList(goastutil.Field("", queriesPointer())),
		goastutil.BlockStmt(statements...),
	)
}

// newWithReplicaFunction returns the NewWithReplica constructor, which runs read-only
// queries on a replica and the rest on the writer.
//
// Takes spec (*QuerierSpec) which describes the querier.
//
// Returns *ast.FuncDecl which is the declaration.
func newWithReplicaFunction(spec *QuerierSpec) *ast.FuncDecl {
	reader, writer := spec.Connections.Reader, spec.Connections.Writer
	var statements []ast.Stmt
	if spec.WrapDBTX {
		statements = append(statements, resolveWrapper(writer))
	}
	statements = append(statements, goastutil.ReturnStmt(
		queriesLiteral(spec, goastutil.CachedIdent(reader), goastutil.CachedIdent(writer), goastutil.CachedIdent(identWrapperField)),
	))
	return goastutil.FuncDecl(
		identNewWithReplica,
		goastutil.FieldList(
			goastutil.Field(writer, goastutil.CachedIdent(identDBTX)),
			goastutil.Field(reader, goastutil.CachedIdent(identDBTX)),
		),
		goastutil.FieldList(goastutil.Field("", queriesPointer())),
		goastutil.BlockStmt(statements...),
	)
}

// resolveWrapper returns `wrapper, _ := <source>.(dbtxWrapper)`.
//
// Takes source (string) which names the connection.
//
// Returns *ast.AssignStmt which is the statement.
func resolveWrapper(source string) *ast.AssignStmt {
	return goastutil.DefineStmtMulti(
		[]string{identWrapperField, identBlank},
		goastutil.TypeAssertExpr(goastutil.CachedIdent(source), goastutil.CachedIdent(identWrapperInterface)),
	)
}

// withTxMethod returns WithTx, which returns a querier running every query on a
// transaction: wrapped by the connection's wrapper when the spec wraps transactions.
//
// Takes spec (*QuerierSpec) which describes the querier; its Transactions is set.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns *ast.FuncDecl which is the declaration.
func withTxMethod(spec *QuerierSpec, tracker *goast.ImportTracker) *ast.FuncDecl {
	transactions := spec.Transactions
	transaction := goastutil.CachedIdent(transactions.Parameter)
	body := []ast.Stmt{goastutil.ReturnStmt(queriesLiteral(spec, transaction, transaction, goastutil.NilIdent()))}
	if spec.WrapDBTX {
		body = wrappedWithTxBody(spec)
	}
	return &ast.FuncDecl{
		Recv: queriesReceiver(),
		Name: goastutil.CachedIdent(identWithTx),
		Type: &ast.FuncType{
			Params:  goastutil.FieldList(goastutil.Field(transactions.Parameter, tracker.AddType(transactions.Type))),
			Results: goastutil.FieldList(goastutil.Field("", queriesPointer())),
		},
		Body: goastutil.BlockStmt(body...),
	}
}

// wrappedWithTxBody returns the body of WithTx for a querier that wraps transactions: a
// nil querier runs on the bare transaction, and any other on the transaction its wrapper
// wraps.
//
// Takes spec (*QuerierSpec) which describes the querier; its Transactions is set.
//
// Returns []ast.Stmt which holds the body.
func wrappedWithTxBody(spec *QuerierSpec) []ast.Stmt {
	transaction := goastutil.CachedIdent(spec.Transactions.Parameter)
	receiver := goastutil.CachedIdent(goast.IdentQueriesReceiver)
	wrapped := goastutil.CachedIdent(goast.IdentDB)
	return []ast.Stmt{
		goastutil.IfStmt(nil,
			&ast.BinaryExpr{X: receiver, Op: token.EQL, Y: goastutil.NilIdent()},
			goastutil.BlockStmt(goastutil.ReturnStmt(queriesLiteral(spec, transaction, transaction, goastutil.NilIdent()))),
		),
		goastutil.DefineStmt(goast.IdentDB, goastutil.CallExpr(
			goastutil.SelectorExprFrom(receiver, identWrapHelper),
			transaction,
		)),
		goastutil.ReturnStmt(queriesLiteral(spec, wrapped, wrapped, goastutil.SelectorExprFrom(receiver, identWrapperField))),
	}
}

// wrapDBTXMethod returns wrapDBTX, which wraps a connection with the querier's wrapper,
// leaving it as it is when there is no wrapper or the wrapped value is no DBTX.
//
// Returns *ast.FuncDecl which is the declaration.
func wrapDBTXMethod() *ast.FuncDecl {
	receiver := goastutil.CachedIdent(goast.IdentQueriesReceiver)
	wrapperField := goastutil.SelectorExprFrom(receiver, identWrapperField)
	connection := goastutil.CachedIdent(goast.IdentDB)
	return &ast.FuncDecl{
		Recv: queriesReceiver(),
		Name: goastutil.CachedIdent(identWrapHelper),
		Type: goastutil.FuncType(
			goastutil.FieldList(goastutil.Field(goast.IdentDB, goastutil.CachedIdent(identDBTX))),
			goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(identDBTX))),
		),
		Body: goastutil.BlockStmt(
			goastutil.IfStmt(nil,
				&ast.BinaryExpr{X: wrapperField, Op: token.EQL, Y: goastutil.NilIdent()},
				goastutil.BlockStmt(goastutil.ReturnStmt(connection)),
			),
			goastutil.DefineStmtMulti(
				[]string{identWrapped, identOK},
				goastutil.TypeAssertExpr(
					goastutil.CallExpr(goastutil.SelectorExprFrom(wrapperField, identWrapMethod), connection),
					goastutil.CachedIdent(identDBTX),
				),
			),
			goastutil.IfStmt(nil,
				&ast.UnaryExpr{Op: token.NOT, X: goastutil.CachedIdent(identOK)},
				goastutil.BlockStmt(goastutil.ReturnStmt(connection)),
			),
			goastutil.ReturnStmt(goastutil.CachedIdent(identWrapped)),
		),
	}
}

// runInTxMethod returns RunInTx, which begins a transaction, runs a function with a
// querier on it, and commits when the function succeeds or rolls back when it fails.
//
// Takes transactions (*TransactionSpec) which describes the transaction helpers.
// Takes tracker (*goast.ImportTracker) which collects the imports the types need.
//
// Returns *ast.FuncDecl which is the declaration.
func runInTxMethod(transactions *TransactionSpec, tracker *goast.ImportTracker) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: queriesReceiver(),
		Name: goastutil.CachedIdent(identRunInTx),
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(goast.IdentCtx, goastutil.SelectorExpr(goast.IdentContext, goast.IdentContextType)),
				parameterField(&transactions.Source, tracker),
				goastutil.Field(identFunction, &ast.FuncType{
					Params:  goastutil.FieldList(goastutil.Field("", queriesPointer())),
					Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
				}),
			),
			Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
		},
		Body: goastutil.BlockStmt(runInTxBody(transactions)...),
	}
}

// runInTxBody returns the body of RunInTx.
//
// Takes transactions (*TransactionSpec) which describes the transaction helpers.
//
// Returns []ast.Stmt which holds the body.
func runInTxBody(transactions *TransactionSpec) []ast.Stmt {
	contextArgument := goastutil.CachedIdent(goast.IdentCtx)
	beginArguments := []ast.Expr{contextArgument}
	if transactions.BeginOptions {
		beginArguments = append(beginArguments, goastutil.NilIdent())
	}
	var endArguments []ast.Expr
	if transactions.EndTakesContext {
		endArguments = []ast.Expr{contextArgument}
	}
	transaction := goastutil.CachedIdent(goast.IdentTransaction)
	failure := goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentErr)))
	return []ast.Stmt{
		goastutil.DefineStmtMulti(
			[]string{goast.IdentTransaction, goast.IdentErr},
			goastutil.CallExpr(
				goastutil.SelectorExprFrom(goastutil.CachedIdent(transactions.Source.Name), transactions.Begin),
				beginArguments...,
			),
		),
		&ast.IfStmt{Cond: errNotNil(), Body: failure},
		&ast.DeferStmt{Call: goastutil.CallExpr(goastutil.SelectorExprFrom(transaction, "Rollback"), endArguments...)},
		goastutil.IfStmt(
			goastutil.AssignStmt(
				goastutil.CachedIdent(goast.IdentErr),
				goastutil.CallExpr(
					goastutil.CachedIdent(identFunction),
					goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), identWithTx), transaction),
				),
			),
			errNotNil(),
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentErr))),
		),
		goastutil.ReturnStmt(goastutil.CallExpr(goastutil.SelectorExprFrom(transaction, "Commit"), endArguments...)),
	}
}

// errNotNil returns `err != nil`.
//
// Returns ast.Expr which is the comparison.
func errNotNil() ast.Expr {
	return &ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)}
}
