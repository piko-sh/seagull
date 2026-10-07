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
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

// pgxReader adapts columns that pgx cannot scan into their mapped Go types. It casts
// values such as bit strings and intervals to text when pgx's binary codecs cannot scan
// them into strings.
type pgxReader struct {
	// types maps the request's SQL types.
	types sqlmodel.GoTypeResolver
}

var _ gogen.ColumnReader = pgxReader{}

// ColumnReading returns how the generated code reads a column of a SQL type: through a
// cast to text when its Go type is a string pgx cannot scan the type's binary form into,
// and as it is otherwise.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns gogen.ColumnReading which says how the column is read.
func (reader pgxReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	decodedApart := sqlType.Category == sqlmodel.TypeCategoryText && sqlType.BinaryForm ||
		sqlType.Temporal == sqlmodel.TemporalKindInterval
	if decodedApart && reader.mapsToString(sqlType) {
		return gogen.ColumnReadAsText
	}
	return gogen.ColumnReadAsIs
}

// mapsToString reports whether a SQL type maps to Go's string.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true when the type's Go type, NOT NULL, is string.
func (reader pgxReader) mapsToString(sqlType sqlmodel.SQLType) bool {
	if reader.types == nil {
		return false
	}
	goType := reader.types.Resolve(sqlType, false)
	return goType.Package == "" && goType.Composite == nil && goType.Name == "string"
}
