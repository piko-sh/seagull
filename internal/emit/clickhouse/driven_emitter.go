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
	"context"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/refusal"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// importDriver is the canonical clickhouse-go/v2 native driver subpackage import path.
	// It is written into generated code only; this module never imports it at build time.
	importDriver = "github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	// importClickHouse is the clickhouse-go/v2 root package, whose Named wraps each bound
	// parameter. It is written into generated code only.
	importClickHouse = "github.com/ClickHouse/clickhouse-go/v2"

	// importShopspringDecimal is the decimal package clickhouse-go/v2 scans Decimal columns
	// into. It is written into generated code only.
	importShopspringDecimal = "github.com/shopspring/decimal"

	// importMathBig is the standard library package for integers wider than 64 bits.
	importMathBig = "math/big"

	// importTime is the standard library package whose Duration type clickhouse-go reads a
	// Time or Time64 value into.
	importTime = "time"
)

// Emitter implements emit.EmitterPort by generating Go source code targeting the native
// clickhouse-go/v2 driver interface. All code generation uses go/ast node construction
// for deterministic, syntactically valid output.
type Emitter struct{}

var (
	_ emit.EmitterPort = (*Emitter)(nil)

	_ emit.TypeDefaulterPort = (*Emitter)(nil)

	_ emit.CheckerPort = (*Emitter)(nil)
)

// New creates a new ClickHouse native code emitter.
//
// Returns *Emitter which is ready to emit Go source code.
func New() *Emitter {
	return &Emitter{}
}

// Emit generates every Go source file for one package targeting the native
// clickhouse-go/v2 driver.
//
// It first refuses a request that cannot be emitted as it is, such as a nil one or one
// whose naming plan does not fit its queries, and then one Check finds an error problem
// with, for a caller that emits without checking, such as a query whose command the
// native driver cannot honour.
//
// Takes request (*emit.Request) which carries the package, schema, queries, type mappings
// and style.
//
// Returns []sqlmodel.GeneratedFile which holds the generated files.
// Returns error when the request is refused, or Check finds an error problem, either
// wrapping emit.ErrRefused, or when code emission fails.
func (emitter *Emitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	if err := refusal.Invalid(generate.ValidateRequest(request)); err != nil {
		return nil, fmt.Errorf("emitting queries: %w", err)
	}
	if err := refusal.Error(emitter.Check(request)); err != nil {
		return nil, fmt.Errorf("emitting queries for %s: %w", request.Package, err)
	}
	copied := *request
	copied.Queries = chvalues.ArrayLists(request.Queries)
	return generate.Package(ctx, &copied, newNativeDriver(&copied).driver())
}

// Check reports requests that the native ClickHouse driver cannot implement.
//
// Checks cover command results, batch shapes, unsupported nested values, driver decoding
// failures and values whose type nests deeper than generated code follows.
//
// Takes request (*emit.Request) which is the request about to be emitted. Check does not
// change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func (*Emitter) Check(request *emit.Request) []emit.Problem {
	problems := emitkit.CheckTypeDepth(request)
	for _, query := range request.Queries {
		problems = append(problems, commandProblems(request, query)...)
		problems = append(problems, documentMapProblems(query)...)
		problems = append(problems, nativeEnums.Problems(request, query, nativeLists, nativeDriverName)...)
		if appendsColumns(query) {
			problems = append(problems, nativeBatchEnums.Problems(request, query, nativeLists, nativeDriverName)...)
			problems = append(problems, appendProblems(query)...)
		}
	}
	return problems
}

// DefaultTypes returns the native driver's natural types for the categories without a
// single Go type: clickhouse-go/v2 scans decimals into shopspring decimals, which it
// already depends on, wide integers into math/big integers, and a Time or Time64 value
// into a time.Duration.
//
// Takes info (*dialect.Facts) which is ignored: the native driver serves ClickHouse only.
//
// Returns sqlmodel.TypeSlots which holds the native driver's slot defaults.
func (*Emitter) DefaultTypes(_ *dialect.Facts) sqlmodel.TypeSlots {
	decimal := sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: importShopspringDecimal, Name: "Decimal"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: importShopspringDecimal, Name: "*Decimal"},
	}
	bigInteger := sqlmodel.GoType{Composite: nil, Package: importMathBig, Name: "*Int"}
	duration := sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: importTime, Name: "Duration"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: importTime, Name: "*Duration"},
	}
	return sqlmodel.TypeSlots{
		Decimal:      decimal,
		Money:        decimal,
		BigInt:       sqlmodel.TypeSlot{NotNull: bigInteger, Nullable: bigInteger},
		JSON:         sqlmodel.TypeSlot{},
		JSONScanInto: sqlmodel.TypeSlot{},
		Interval:     sqlmodel.TypeSlot{},
		TimeOfDay:    duration,
		IPAddress:    sqlmodel.TypeSlot{},
		Map:          sqlmodel.TypeSlot{},
		Struct:       sqlmodel.TypeSlot{},
		Enum:         sqlmodel.TypeSlot{},
	}
}

// commandProblems returns the problems the native driver has with a query's command.
//
// These include execrows or execresult commands, whose results it does not report; a
// batch whose VALUES row cannot be repeated per row or binds more named values than one
// statement holds; and a copyfrom whose rows cannot be appended as columns.
//
// Takes request (*emit.Request) which describes the dialect. commandProblems does not
// change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func commandProblems(request *emit.Request, query *sqlmodel.AnalysedQuery) []emit.Problem {
	var returns string
	switch query.Command {
	case sqlmodel.QueryCommandBatch:
		if appendsColumns(query) {
			return emitkit.CheckBatch(query, emitkit.BatchRows)
		}
		return emitkit.CheckBatch(query, emitkit.BatchRows, emitkit.BindLimit{
			Form: request.Dialect.Placeholders.Execution, Limit: batchLimit(&request.Dialect),
		})
	case sqlmodel.QueryCommandCopyFrom:
		return emitkit.CheckBatch(query, emitkit.BatchColumns)
	case sqlmodel.QueryCommandExecRows:
		returns = "returns the number of rows the statement changed"
	case sqlmodel.QueryCommandExecResult:
		returns = "returns the sql.Result counting the rows the statement changed"
	default:
		return nil
	}
	return []emit.Problem{{
		Query: query,
		Code:  diag.CodeEmitterUnsupported,
		Message: fmt.Sprintf("command: %s %s, which the native ClickHouse driver does not report, since its Exec "+
			"returns only an error; use command: exec", query.Command, returns),
		Severity: diag.SeverityError,
	}}
}

// appendsColumns reports whether the generated method of a query appends its rows to a
// native batch column by column: a copyfrom, and a batch whose VALUES row is lone
// placeholders.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query's values are appended as columns.
func appendsColumns(query *sqlmodel.AnalysedQuery) bool {
	switch query.Command {
	case sqlmodel.QueryCommandCopyFrom:
		return true
	case sqlmodel.QueryCommandBatch:
		_, columns := emitkit.RowColumns(query)
		return columns
	default:
		return false
	}
}

// bindsParameters reports whether any query binds a parameter through clickhouse.Named,
// and so needs the generated value helper. A copyfrom query, and a batch query whose
// VALUES row is one placeholder per column, append typed values to a columnar batch
// instead, so they need none.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the analysed queries.
//
// Returns bool which is true when the value helper is needed.
func bindsParameters(queries []*sqlmodel.AnalysedQuery) bool {
	for _, query := range queries {
		if len(query.Parameters) == 0 {
			continue
		}
		if _, columns := emitkit.RowColumns(query); columns && query.Command == sqlmodel.QueryCommandBatch {
			continue
		}
		if query.Command == sqlmodel.QueryCommandCopyFrom {
			continue
		}
		return true
	}
	return false
}
