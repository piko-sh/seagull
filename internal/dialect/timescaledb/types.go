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

import "seagull.sh/seagull/internal/sqlmodel"

const (
	// widthRegproc is the width in bits of regproc, an unsigned object identifier.
	widthRegproc = 32
)

// timescaleDBTypes holds the opaque aggregate-state and hyperfunction types TimescaleDB
// exposes, keyed by lower-cased engine name.
//
// Aggregate states are registered as TypeCategoryUnknown with a stable EngineName;
// regproc is an unsigned integer type. The aggregate states' concrete Go representation
// is opaque to codegen. Users can supply sgl.column(go_type: ...) overrides for concrete
// Go destinations in projects that materialise the aggregate state in application code.
//
// The entries come in three groups: the core aggregate-state types (stats summaries,
// time-weighted summary, candlestick, state, compact_state, hyperloglog, tdigest,
// uddsketch and heartbeat); the long-tail toolkit states (frequency-analysis space-saving
// states, min_n states, the timevector summary, count_min_sketch, and the composite
// records hypertable management functions return); and the system-type aliases
// TimescaleDB exposes through its catalogue that the host postgres engine does not
// bootstrap on its own, each mirroring a known builtin so the analyser treats it as a
// recognised type rather than reporting an unknown identifier.
var timescaleDBTypes = map[string]sqlmodel.SQLType{
	"statssummary1d":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "statssummary1d"),
	"statssummary2d":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "statssummary2d"),
	"counter_summary": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "counter_summary"),
	"gauge_summary":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "gauge_summary"),

	"time_weight_summary": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "time_weight_summary"),

	"candlestick":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "candlestick"),
	"state_summary": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "state_summary"),

	"compact_state_agg": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "compact_state_agg"),

	"hyperloglog": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "hyperloglog"),
	"tdigest":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "tdigest"),
	"uddsketch":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "uddsketch"),
	"heartbeat":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "heartbeat"),

	"create_hypertable_record": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "create_hypertable_record"),
	"add_dimension_record":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "add_dimension_record"),

	"space_saving_aggregate":        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "space_saving_aggregate"),
	"space_saving_bigint_aggregate": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "space_saving_bigint_aggregate"),
	"space_saving_text_aggregate":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "space_saving_text_aggregate"),

	"min_n_state":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "min_n_state"),
	"min_n_by_state": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "min_n_by_state"),

	"timevector_tstz_f64": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "timevector_tstz_f64"),

	"count_min_sketch": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "count_min_sketch"),

	"regproc": sqlmodel.NewIntegerType("regproc", widthRegproc, true),
}

// accessorTypeNames lists the toolkit's accessor types, each the type an accessor such as
// average() or approx_percentile(0.95) returns, which the toolkit's -> operator applies
// to an aggregate state, as the toolkit 1.22 declares them (postgres.WithAccessorArrows).
var accessorTypeNames = []string{
	"accessorapproxpercentile", "accessorapproxpercentilerank", "accessoraverage", "accessoraveragex",
	"accessoraveragey", "accessorclose", "accessorclosetime", "accessorcorr", "accessorcounterzerotime",
	"accessorcovar", "accessordeadranges", "accessordelta", "accessordeterminationcoeff", "accessordistinctcount",
	"accessordowntime", "accessordurationin", "accessordurationinint", "accessordurationinrange",
	"accessordurationinrangeint", "accessorerror", "accessorextrapolateddelta", "accessorextrapolatedrate",
	"accessorfirsttime", "accessorfirstval", "accessorhigh", "accessorhightime", "accessorideltaleft",
	"accessorideltaright", "accessorintegral", "accessorintercept", "accessorinterpolateddurationin",
	"accessorinterpolateddurationinint", "accessorinterpolatedstateinttimeline", "accessorinterpolatedstateperiods",
	"accessorinterpolatedstateperiodsint", "accessorinterpolatedstatetimeline", "accessorintoarray",
	"accessorintointvalues", "accessorintovalues", "accessorirateleft", "accessorirateright", "accessorkurtosis",
	"accessorkurtosisx", "accessorkurtosisy", "accessorlasttime", "accessorlastval", "accessorliveat",
	"accessorliveranges", "accessorlow", "accessorlowtime", "accessormaxfrequencyint", "accessormaxval",
	"accessormean", "accessorminfrequencyint", "accessorminval", "accessornumchanges", "accessornumelements",
	"accessornumgaps", "accessornumliveranges", "accessornumresets", "accessornumvals", "accessoropen",
	"accessoropentime", "accessorpercentilearray", "accessorrate", "accessorskewness", "accessorskewnessx",
	"accessorskewnessy", "accessorslope", "accessorstateat", "accessorstateatint", "accessorstateinttimeline",
	"accessorstateperiods", "accessorstateperiodsint", "accessorstatetimeline", "accessorstddev", "accessorstddevx",
	"accessorstddevy", "accessorstderror", "accessorsum", "accessorsumx", "accessorsumy", "accessortimedelta",
	"accessortopn", "accessortopncount", "accessorunnest", "accessoruptime", "accessorvariance", "accessorvariancex",
	"accessorvariancey", "accessorwithbounds", "accessorxintercept", "counterinterpolateddeltaaccessor",
	"counterinterpolatedrateaccessor", "heartbeatinterpolateaccessor", "heartbeatinterpolateddowntimeaccessor",
	"heartbeatinterpolateduptimeaccessor", "heartbeattrimtoaccessor", "timeweightinterpolatedaverageaccessor",
	"timeweightinterpolatedintegralaccessor",
}

// accessorTypes holds the accessor types as opaque types, keyed by name, so the accessors
// in functions.spec name known types.
var accessorTypes = opaqueTypes(accessorTypeNames)

// opaqueTypes returns opaque types of the given names: each of the unknown category,
// keyed by its name, as timescaleDBTypes holds the aggregate states.
//
// Takes names ([]string) which name the types.
//
// Returns map[string]sqlmodel.SQLType which holds the types.
func opaqueTypes(names []string) map[string]sqlmodel.SQLType {
	types := make(map[string]sqlmodel.SQLType, len(names))
	for _, name := range names {
		types[name] = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
	}
	return types
}
