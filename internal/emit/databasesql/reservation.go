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
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/multirow"
	"seagull.sh/seagull/internal/emit/internal/reservation"
	"seagull.sh/seagull/internal/gogen"
)

var (
	_ emit.ReserverPort = (*Emitter)(nil)

	// preparedLocals lists the arguments in scope where prepared.go names the SQL constants
	// of the static queries.
	preparedLocals = []string{"ctx", "db"}

	// driverHelpers lists the suffixes of the driver helpers the emitter declares besides
	// those of the default reservation.
	driverHelpers = []string{"DriverConvert", "DriverRefusal"}
)

// Reserve returns names used by generated database/sql code and its required helpers.
//
// Takes info (*dialect.Facts) which describes the dialect. Reserve does not change it.
// Takes style (*emit.Style) which holds the helper and error prefixes. Reserve does not
// change it.
//
// Returns emit.Reservation which holds the names.
func (emitter *Emitter) Reserve(info *dialect.Facts, style *emit.Style) emit.Reservation {
	locals := slices.Concat(multirow.Locals, preparedLocals)
	helpers := slices.Concat(chvalues.Helpers, chvalues.EncodedHelpers, chvalues.NullableHelpers, chvalues.DurationHelpers, chvalues.DocumentHelpers, driverHelpers)
	files := []string{chvalues.File, chvalues.DocumentsFile}
	profile := profileFor(info.Drivers, emitter.settings.driver)
	if profile.enums.Scan != 0 {
		locals = slices.Concat(locals, reservation.EnumLocals)
	}
	if profile.enums != (gogen.EnumStrings{}) {
		helpers = slices.Concat(helpers, reservation.EnumHelpers)
		files = slices.Concat(files, reservation.EnumFiles)
	}
	return reservation.For(info, style, locals, reservation.Helpers(style, helpers, chvalues.Sentinels), files)
}
