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
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/multirow"
	"seagull.sh/seagull/internal/emit/internal/reservation"
	"seagull.sh/seagull/internal/emit/internal/valuereading"
)

var (
	_ emit.ReserverPort = (*Emitter)(nil)

	// batchLocals lists the locals the batch methods declare besides the shared ones: those
	// of the multi-row INSERT. The native batch prepares the query's SQL constant in the
	// statement that declares the batch, and its loop refers to no planned name, so neither
	// name needs keeping clear.
	batchLocals = multirow.Locals
)

// Reserve returns the names used by the emitter's generated code and helpers.
//
// Takes info (*dialect.Facts) which describes the dialect. Reserve does not change it.
// Takes style (*emit.Style) which holds the helper and error prefixes. Reserve does not
// change it.
//
// Returns emit.Reservation which holds the names.
func (*Emitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	return reservation.For(info, style, slices.Concat(batchLocals, reservation.EnumLocals),
		reservation.Helpers(style,
			slices.Concat(chvalues.Helpers, chvalues.EncodedHelpers, chvalues.NullableHelpers, chvalues.DurationHelpers, compositeHelpers, documentHelpers, entriesHelpers, chvalues.DocumentHelpers,
				chvalues.AppendingHelpers, valuereading.Helpers, reservation.EnumHelpers),
			slices.Concat(chvalues.Sentinels, chvalues.AppendingSentinels)),
		slices.Concat([]string{chvalues.File, compositesFile, valuereading.File, chvalues.DocumentsFile, chvalues.AppendingFile}, reservation.EnumFiles))
}
