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

package timescaledb

import (
	_ "embed"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// anyTypeSpelling is the type spelling functions.spec gives the wildcard type that
	// accepts every argument type.
	anyTypeSpelling = "any"

	// arrayTypeSuffix ends a type spelling of functions.spec that names an array of the type
	// before it, such as regclass[].
	arrayTypeSuffix = "[]"

	// regprocTypeSpelling is the functions.spec spelling of the procedure reference add_job
	// and alter_job take.
	regprocTypeSpelling = "regproc"
)

var (
	// functionSpecSource is the TimescaleDB function catalogue, one signature per line in
	// the format dialectkit.FunctionSpec describes.
	//
	//go:embed functions.spec
	functionSpecSource string

	// functionSpec decodes functionSpecSource the first time a catalogue is built and keeps
	// the decode error, which every dialect built from it reports; the signatures that
	// decoded are registered regardless.
	functionSpec = dialectkit.EmbeddedFunctionSpec(functionSpecSource)
)

// registerTimescaleDBFunctions registers the TimescaleDB signatures of functions.spec
// with the postgres function-catalogue builder, after the PostgreSQL built-ins.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the registered
// TimescaleDB signatures.
func registerTimescaleDBFunctions(builder *postgres.FunctionCatalogueBuilder) {
	registerFunctionSpec(builder, functionSpec)
}

// registerFunctionSpec registers the signatures loadSpecification gives with the builder,
// and reports to the builder the error decoding them met, so the dialect returns it from
// every ApplyDDL and AnalyseQuery; the signatures that decoded are registered regardless.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the signatures.
// Takes loadSpecification (func() (*dialectkit.FunctionSpec, error)) which gives the
// decoded signatures.
func registerFunctionSpec(builder *postgres.FunctionCatalogueBuilder, loadSpecification func() (*dialectkit.FunctionSpec, error)) {
	specification, err := loadSpecification()
	if err != nil {
		builder.ReportError(fmt.Errorf("registering the TimescaleDB functions: %w", err))
	}
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			return specType(builder, spelling)
		},
		DefaultNullability: nullOnNull,
		Add:                builder.Add,
	})
}

// specType resolves a type spelling of functions.spec.
//
// The wildcard spelling resolves to the wildcard type and a spelling ending in [] to an
// array of the type before it. regproc resolves to a text-categorised type rather than
// the unsigned object identifier a regproc column has: like regclass, it is an oid alias
// whose canonical call form is a string literal, such as add_job('my_proc', ...), so the
// Text category lets those calls resolve. A row type recordFields lists resolves to a
// composite of its columns. Every other spelling resolves through the builder's type
// table, extra types included.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) whose type table resolves the
// spelling.
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func specType(builder *postgres.FunctionCatalogueBuilder, spelling string) sqlmodel.SQLType {
	switch {
	case spelling == anyTypeSpelling:
		return builder.Any()
	case spelling == regprocTypeSpelling:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, regprocTypeSpelling)
	case recordFields[spelling] != nil:
		record, _ := recordType(builder, spelling)
		return record
	case strings.HasSuffix(spelling, arrayTypeSuffix):
		element := specType(builder, strings.TrimSuffix(spelling, arrayTypeSuffix))
		array := sqlmodel.SQLType{}
		array.Category = sqlmodel.TypeCategoryArray
		array.EngineName = element.EngineName + arrayTypeSuffix
		array.ElementType = &element
		return array
	default:
		return builder.Type(spelling)
	}
}

// nullOnNull is the NULL behaviour of a TimescaleDB signature whose spec line names none:
// NULL when any argument is NULL.
//
// Returns sqlmodel.FunctionNullableBehaviour which is FunctionNullableReturnsNullOnNull.
func nullOnNull(_ string, _ bool) sqlmodel.FunctionNullableBehaviour {
	return sqlmodel.FunctionNullableReturnsNullOnNull
}
