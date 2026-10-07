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

package main

import (
	"context"
	"fmt"
	"go/format"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// indexFile is the name of the file the emitter adds.
	indexFile = "query_index.go"

	// infoType is the type the index declares for one query.
	infoType = "QueryInfo"

	// indexVariable is the variable the index declares.
	indexVariable = "QueryIndex"
)

// indexEmitter wraps another emitter and adds query_index.go, which lists every query of
// the package with its method, its command and whether it only reads. 052_custom_emitter
// explains it.
type indexEmitter struct {
	// base generates the package itself.
	base emit.EmitterPort
}

var (
	_ emit.EmitterPort = (*indexEmitter)(nil)

	_ emit.TypeDefaulterPort = (*indexEmitter)(nil)

	_ emit.ReserverPort = (*indexEmitter)(nil)

	_ emit.CheckerPort = (*indexEmitter)(nil)

	_ emit.NullElementBinderPort = (*indexEmitter)(nil)
)

// Emit generates the wrapped emitter's files and appends the index.
//
// Takes request (*emit.Request) which carries the analysed queries, the naming plan and
// the style.
//
// Returns []emit.GeneratedFile which holds the wrapped emitter's files and the index.
// Returns error when either cannot be generated.
func (e indexEmitter) Emit(ctx context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
	files, err := e.base.Emit(ctx, request)
	if err != nil {
		return nil, err
	}
	var source strings.Builder
	fmt.Fprintf(&source, "%s\n\npackage %s\n\n", request.Style.Header, request.Package)
	fmt.Fprintf(&source, "// %s describes one generated query method.\n", infoType)
	fmt.Fprintf(&source, "type %s struct {\n\tName string\n\tMethod string\n\tCommand string\n\tReadOnly bool\n}\n\n", infoType)
	fmt.Fprintf(&source, "// %s lists every query of the package in source order.\n", indexVariable)
	fmt.Fprintf(&source, "var %s = []%s{\n", indexVariable, infoType)
	for index, query := range request.Queries {
		fmt.Fprintf(&source, "\t{Name: %q, Method: %q, Command: %q, ReadOnly: %t},\n",
			query.Name, request.Naming.Queries[index].Method, query.Command, query.ReadOnly)
	}
	source.WriteString("}\n")
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", indexFile, err)
	}
	return append(files, emit.GeneratedFile{Name: indexFile, Content: formatted}), nil
}

// DefaultTypes passes on the wrapped emitter's type defaults, which the wrapper would
// otherwise hide.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns seagull.TypeSlots which holds the wrapped emitter's defaults, or none.
func (e indexEmitter) DefaultTypes(info *dialect.Facts) seagull.TypeSlots {
	if defaulter, ok := e.base.(emit.TypeDefaulterPort); ok {
		return defaulter.DefaultTypes(info)
	}
	return seagull.TypeSlots{}
}

// Reserve passes on the names the wrapped emitter's code uses and adds the index's, so
// the naming plan never chooses them for anything else.
//
// Takes info (*dialect.Facts) which describes the dialect.
// Takes style (*emit.Style) which holds the style of the generated code.
//
// Returns emit.Reservation which holds the wrapped emitter's names and the index's.
func (e indexEmitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	reservation := emit_kit.ReservationFor(e.base, info, style)
	reservation.Declarations = append(reservation.Declarations, infoType, indexVariable)
	reservation.Files = append(reservation.Files, indexFile)
	return reservation
}

// Check passes on what the wrapped emitter refuses, such as go.prepared on a driver that
// cannot prepare the package's queries, which sgl reports as diagnostics.
//
// Takes request (*emit.Request) which is the request about to be emitted.
//
// Returns []emit.Problem which holds the wrapped emitter's problems, or none.
func (e indexEmitter) Check(request *emit.Request) []emit.Problem {
	if checker, ok := e.base.(emit.CheckerPort); ok {
		return checker.Check(request)
	}
	return nil
}

// BindsNullElements passes on whether the wrapped emitter's driver binds a NULL element
// of an array parameter. Without it the generator would take the wrapper for one that
// does, and give array parameters nullable elements on a driver that panics on them, such
// as DuckDB's.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns bool which is the wrapped emitter's answer, or true when it gives none.
func (e indexEmitter) BindsNullElements(info *dialect.Facts) bool {
	if binder, ok := e.base.(emit.NullElementBinderPort); ok {
		return binder.BindsNullElements(info)
	}
	return true
}
