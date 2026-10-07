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
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

// recordField is a column of a row type a set-returning function returns.
type recordField struct {
	// name is the column's name.
	name string

	// spelling is the column's type as functions.spec spells types.
	spelling string
}

var (
	// byteCountFields are the size columns hypertable_detailed_size and its relatives
	// report, in order.
	byteCountFields = []recordField{
		{name: "table_bytes", spelling: "int8"},
		{name: "index_bytes", spelling: "int8"},
		{name: "toast_bytes", spelling: "int8"},
		{name: "total_bytes", spelling: "int8"},
	}

	// compressionCountFields are the size columns the compression statistics report before
	// and after compression, in order.
	compressionCountFields = []recordField{
		{name: "before_compression_table_bytes", spelling: "int8"},
		{name: "before_compression_index_bytes", spelling: "int8"},
		{name: "before_compression_toast_bytes", spelling: "int8"},
		{name: "before_compression_total_bytes", spelling: "int8"},
		{name: "after_compression_table_bytes", spelling: "int8"},
		{name: "after_compression_index_bytes", spelling: "int8"},
		{name: "after_compression_toast_bytes", spelling: "int8"},
		{name: "after_compression_total_bytes", spelling: "int8"},
	}

	// nodeNameField is the column naming the data node a size was measured on.
	nodeNameField = recordField{name: "node_name", spelling: "name"}

	// periodFields are the bounds of a period a state accessor reports.
	periodFields = []recordField{{name: "start_time", spelling: "timestamptz"}, {name: "end_time", spelling: "timestamptz"}}

	// recordFields lists the columns of each row type functions.spec names as the result of
	// a set-returning function, as TimescaleDB 2.27 and the toolkit 1.22 declare them in
	// their RETURNS TABLE clauses.
	recordFields = map[string][]recordField{
		"heartbeat_range_record":                      {{name: "start", spelling: "timestamptz"}, {name: "end", spelling: "timestamptz"}},
		"state_duration_record":                       {{name: "state", spelling: "text"}, {name: "duration", spelling: "interval"}},
		"state_timeline_record":                       append([]recordField{{name: "state", spelling: "text"}}, periodFields...),
		"state_int_timeline_record":                   append([]recordField{{name: "state", spelling: "int8"}}, periodFields...),
		"state_period_record":                         periodFields,
		"state_int_period_record":                     periodFields,
		"timevector_point_record":                     {{name: "time", spelling: "timestamptz"}, {name: "value", spelling: "float8"}},
		"min_n_by_record":                             {{name: "value", spelling: anyTypeSpelling}, {name: "data", spelling: anyTypeSpelling}},
		"hypertable_detailed_size_record":             append(append([]recordField{}, byteCountFields...), nodeNameField),
		"hypertable_approximate_detailed_size_record": byteCountFields,
		"chunk_detailed_size_record": append(append([]recordField{{name: "chunk_schema", spelling: "name"},
			{name: "chunk_name", spelling: "name"}}, byteCountFields...), nodeNameField),
		"hypertable_compression_stats_record": append(append([]recordField{{name: "total_chunks", spelling: "int8"},
			{name: "number_compressed_chunks", spelling: "int8"}}, compressionCountFields...), nodeNameField),
		"chunk_compression_stats_record": append(append([]recordField{{name: "chunk_schema", spelling: "name"},
			{name: "chunk_name", spelling: "name"}, {name: "compression_status", spelling: "text"}},
			compressionCountFields...), nodeNameField),
		"space_saving_bigint_aggregate_into_values_record": frequencyFields("int8"),
		"space_saving_text_aggregate_into_values_record":   frequencyFields("text"),
		"space_saving_aggregate_into_values_record":        frequencyFields(anyTypeSpelling),
	}
)

// frequencyFields returns the columns into_values reports for a frequency aggregate: each
// value with the lowest and highest frequency it may have.
//
// Takes valueSpelling (string) which is the type of the counted values.
//
// Returns []recordField which holds the columns.
func frequencyFields(valueSpelling string) []recordField {
	return []recordField{
		{name: "value", spelling: valueSpelling},
		{name: "min_freq", spelling: "float8"},
		{name: "max_freq", spelling: "float8"},
	}
}

// recordType returns the row type functions.spec names with a spelling of recordFields: a
// composite whose fields are the row's columns, so a call of a function returning a set
// of it in a FROM clause has those columns.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which resolves the field types.
// Takes spelling (string) which is the row type's spelling.
//
// Returns sqlmodel.SQLType which is the row type.
// Returns bool which is false when the spelling names no listed row type.
func recordType(builder *postgres.FunctionCatalogueBuilder, spelling string) (sqlmodel.SQLType, bool) {
	fields, listed := recordFields[spelling]
	if !listed {
		return sqlmodel.SQLType{}, false
	}
	record := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, spelling)
	record.StructFields = make([]sqlmodel.StructField, len(fields))
	for index, field := range fields {
		record.StructFields[index] = sqlmodel.StructField{Name: field.name, SQLType: specType(builder, field.spelling), Positional: false}
	}
	return record, true
}
