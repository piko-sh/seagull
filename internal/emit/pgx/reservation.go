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
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/reservation"
)

var (
	_ emit.ReserverPort = (*Emitter)(nil)

	// batchLocals lists the locals the batch method declares besides the shared ones, in
	// scope where it queues the query's SQL constant: the batch and the loop's item. The
	// copyfrom method's row index refers to no planned name, so it stays free.
	batchLocals = []string{"batch", "item"}
)

// Reserve returns the names the emitter's generated code uses: the default reservation's
// package-level names, imports, methods and files, the conversion of enum slices to Go's
// own strings, and the locals its method bodies declare, among them the raw values an
// enum slice is scanned into.
//
// Takes info (*dialect.Facts) which describes the dialect. Reserve does not change it.
// Takes style (*emit.Style) which holds the helper and error prefixes. Reserve does not
// change it.
//
// Returns emit.Reservation which holds the names.
func (*Emitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	return reservation.For(info, style, slices.Concat(batchLocals, reservation.EnumLocals),
		reservation.Helpers(style, reservation.EnumHelpers, nil), reservation.EnumFiles)
}
