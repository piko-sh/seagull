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

// Package reservation builds the names each built-in emitter reserves from the naming
// plan: the default reservation's package-level names, imports, methods and files, which
// cover every built-in emitter, with only the locals the emitter's own generated bodies
// declare, so a query's parameters keep their names wherever the emitter allows.
package reservation

import (
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
)

var (
	// sharedLocals lists local names reserved by the shared method builders.
	//
	// The naming plan avoids these where a local could shadow a planned declaration or
	// argument. Reservation tests check the list against generated code.
	sharedLocals = []string{
		"args", "builder", "ctx", "entry", "err", "exists", "filter", "groupIndex",
		"pagination", "query", "results", "row", "rows", "yield",
	}

	// EnumLocals lists the locals the shared method builders declare for a driver that scans
	// an enum type the package declares through Go's own strings: the raw values a row is
	// scanned into before they are converted.
	EnumLocals = []string{goast.IdentRaw}

	// EnumHelpers lists the suffixes of the helpers the generated code calls for a driver
	// that binds or scans a slice, a slice of pointers or a map of an enum type the package
	// declares through Go's own strings, and reads an optional one through its pointer.
	EnumHelpers = []string{
		gogen.SuffixConvertStrings, gogen.SuffixConvertStringPointers, gogen.SuffixConvertStringMap, gogen.SuffixDereference,
	}

	// EnumFiles lists the files holding those helpers.
	EnumFiles = []string{helpers.ConvertStringsFile}
)

// For returns the reservation of a built-in emitter: the default reservation, whose
// package-level names, imports, methods and files cover every built-in emitter, with the
// locals of the shared method builders and the emitter's own, and the emitter's own
// package-level names and files added.
//
// Takes info (*dialect.Facts) which describes the dialect. For does not change it.
// Takes style (*emit.Style) which holds the helper and error prefixes. For does not
// change it.
// Takes locals ([]string) which are the locals the emitter's own builders declare.
// Takes declarations ([]string) which are the emitter's own package-level names.
// Takes files ([]string) which are the emitter's own files.
//
// Returns emit.Reservation which holds the names, each listed once.
func For(info *dialect.Facts, style *emit.Style, locals, declarations, files []string) emit.Reservation {
	reservation := emitkit.DefaultReservation(info, style)
	reservation.Locals = unique(sharedLocals, locals)
	reservation.Declarations = unique(reservation.Declarations, declarations)
	reservation.Files = unique(reservation.Files, files)
	return reservation
}

// Helpers returns the names of the helpers and sentinel errors a style gives the
// suffixes, such as sglClickHouseValue and errSglClickHouseValue.
//
// Takes style (*emit.Style) which holds the helper prefix. Helpers does not change it.
// Takes suffixes ([]string) which are the helper suffixes.
// Takes sentinels ([]string) which are the sentinel error suffixes.
//
// Returns []string which holds the names.
func Helpers(style *emit.Style, suffixes, sentinels []string) []string {
	var request emit.Request
	request.Style = *style
	vocabulary := gogen.NewVocabulary(&request)
	names := make([]string, 0, len(suffixes)+len(sentinels))
	for _, suffix := range suffixes {
		names = append(names, vocabulary.Helper(suffix))
	}
	for _, suffix := range sentinels {
		names = append(names, vocabulary.Sentinel(suffix))
	}
	return names
}

// unique returns the names of lists, each once, sorted.
//
// Takes lists (...[]string) which hold the names.
//
// Returns []string which holds them.
func unique(lists ...[]string) []string {
	names := slices.Concat(lists...)
	slices.Sort(names)
	return slices.Compact(names)
}
