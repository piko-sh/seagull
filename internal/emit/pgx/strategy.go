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

package pgx

import (
	"go/ast"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// pgxLists is how the generated code writes a slice's list for pgx: bound whole, as one
	// array, where the dialect has an array form.
	pgxLists = emitkit.ListArray
)

var (
	// pgxConnection says how the generated code calls a pgx connection.
	pgxConnection = gogen.Connection{
		ExecResult:         &sqlmodel.GoType{Composite: nil, Package: importPgconn, Name: "CommandTag"},
		Query:              "Query",
		QueryRow:           "QueryRow",
		Exec:               "Exec",
		Reader:             goast.IdentDB,
		Writer:             goast.IdentDB,
		CountScan:          "",
		NoRows:             sqlmodel.GoType{Composite: nil, Package: importPgx, Name: "ErrNoRows"},
		RowsAffectedErrors: false,
	}

	// contextParameter is the context every DBTX method takes first.
	contextParameter = emitkit.DBTXParameter{
		Name:     goast.IdentCtx,
		Type:     sqlmodel.GoType{Composite: nil, Package: "context", Name: "Context"},
		Variadic: false,
	}

	// statementParameters are the parameters every DBTX method running a statement takes.
	statementParameters = []emitkit.DBTXParameter{
		contextParameter,
		{Name: "sql", Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, Variadic: false},
		{Name: "arguments", Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "any"}, Variadic: true},
	}

	// pgxQuerier describes the querier of a pgx package: one connection, which can be a
	// pool, a connection or a transaction, and transactions begun on it.
	pgxQuerier = emitkit.QuerierSpec{
		Transactions: &emitkit.TransactionSpec{
			Type:      pgxType("Tx"),
			Parameter: "tx",
			Begin:     "Begin",
			Source: emitkit.DBTXParameter{
				Name:     "pool",
				Type:     sqlmodel.GoType{Composite: nil, Package: "", Name: identDBTX},
				Variadic: false,
			},
			BeginOptions:    false,
			EndTakesContext: true,
		},
		Connections: emitkit.Connections{Reader: goast.IdentDB, Writer: goast.IdentDB},
		DBTX: []emitkit.DBTXMethod{
			{Name: "Exec", Parameters: statementParameters, Results: []sqlmodel.GoType{
				{Composite: nil, Package: importPgconn, Name: "CommandTag"}, errorType,
			}},
			{Name: "Query", Parameters: statementParameters, Results: []sqlmodel.GoType{pgxType("Rows"), errorType}},
			{Name: "QueryRow", Parameters: statementParameters, Results: []sqlmodel.GoType{pgxType("Row")}},
			{Name: "SendBatch", Parameters: []emitkit.DBTXParameter{
				contextParameter,
				{Name: goast.IdentBatch, Type: pgxType("*Batch"), Variadic: false},
			}, Results: []sqlmodel.GoType{pgxType("BatchResults")}},
			{Name: "CopyFrom", Parameters: []emitkit.DBTXParameter{
				contextParameter,
				{Name: "tableName", Type: pgxType("Identifier"), Variadic: false},
				{Name: "columnNames", Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "[]string"}, Variadic: false},
				{Name: "rowSrc", Type: pgxType("CopyFromSource"), Variadic: false},
			}, Results: []sqlmodel.GoType{{Composite: nil, Package: "", Name: "int64"}, errorType}},
			{Name: "Begin", Parameters: []emitkit.DBTXParameter{contextParameter}, Results: []sqlmodel.GoType{pgxType("Tx"), errorType}},
		},
		Replica:     false,
		PackageNote: "",
		WrapDBTX:    false,
	}

	// errorType is the built-in error type.
	errorType = sqlmodel.GoType{Composite: nil, Package: "", Name: identError}
)

// pgxEnums describes the conversions needed for generated enums in containers.
//
// pgx can bind a scalar enum as text, but containers need conversion to ordinary strings
// unless their types have been registered on the connection.
var pgxEnums = gogen.EnumStrings{
	Scan: gogen.EnumSlice | gogen.EnumMap | gogen.EnumNested | gogen.EnumNestedInMap,
	Bind: gogen.EnumSlice | gogen.EnumMap | gogen.EnumNested | gogen.EnumNestedInMap,
}

// pgxBatch generates the batch and copyfrom methods of a pgx package.
type pgxBatch struct {
	// binder binds each row's parameters as the package's other methods bind them.
	binder pgxBinder

	// vocabulary names the generated helpers of the request.
	vocabulary gogen.Vocabulary
}

var _ gogen.BatchBuilder = (*pgxBatch)(nil)

// Batch returns a batch method that queues one statement per row on a pgx.Batch.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the statement expands a slice parameter.
func (b *pgxBatch) Batch(query *gogen.Query) (ast.Decl, error) {
	method, err := buildBatchMethod(query, b.vocabulary, b.binder)
	if err != nil {
		return nil, err
	}
	return method, nil
}

// RepeatsRow reports that a batch method queues the query's SQL as it is, once per row.
//
// Returns bool which is always false.
func (*pgxBatch) RepeatsRow(_ *gogen.Query) bool {
	return false
}

// CopyStatement reports that a copyfrom method runs no SQL text: it streams the rows
// through pgx's CopyFrom, the COPY protocol.
//
// Returns gogen.CopyStatement which is always gogen.CopyStatementNone.
func (*pgxBatch) CopyStatement(_ *gogen.Query) gogen.CopyStatement {
	return gogen.CopyStatementNone
}

// CopyFrom returns a copyfrom method that streams the rows through pgx's CopyFrom.
//
// Takes query (*gogen.Query) which is the query being generated.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the query's VALUES row cannot be sent as columns.
func (b *pgxBatch) CopyFrom(query *gogen.Query) (ast.Decl, error) {
	method, err := buildCopyFromMethod(query, b.binder)
	if err != nil {
		return nil, err
	}
	return method, nil
}

// pgxType returns a type of the pgx package.
//
// Takes name (string) which is the type's name, with a * or [] prefix for a pointer or
// slice.
//
// Returns sqlmodel.GoType which is the type.
func pgxType(name string) sqlmodel.GoType {
	return sqlmodel.GoType{Composite: nil, Package: importPgx, Name: name}
}

// pgxDriver describes how a pgx package binds and reads values, including arrays, enums,
// binary overrides and casts to text. It also supplies pgx's batch and copyfrom
// generators.
//
// Takes request (*emit.Request) which supplies the vocabulary the helpers are named in.
//
// Returns *gogen.Driver which describes pgx.
func pgxDriver(request *emit.Request) *gogen.Driver {
	binder := pgxBinder{types: request.Types, vocabulary: gogen.NewVocabulary(request)}
	return &gogen.Driver{
		Binder:     binder,
		Reader:     pgxReader{types: request.Types},
		Helpers:    binder,
		Batch:      &pgxBatch{vocabulary: binder.vocabulary, binder: binder},
		Querier:    pgxQuerier,
		Connection: pgxConnection,
		Lists:      pgxLists,
		Enums:      pgxEnums,
	}
}
