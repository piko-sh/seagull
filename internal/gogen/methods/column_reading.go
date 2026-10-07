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

package methods

import (
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/gogen"
)

var (
	// errUnwrappableColumn reports a column the driver can only read as JSON that the
	// query's SQL cannot wrap.
	errUnwrappableColumn = errors.New("column cannot be auto-decoded in this query shape")
)

// CheckColumnsReadable rejects columns that require JSON conversion but cannot be wrapped
// in the query template.
//
// This prevents generated code from failing when it scans the driver's raw values.
//
// Takes emission (*gogen.Emission) which says how the driver reads each column.
// Takes query (*gogen.Query) which holds the columns and the ones its SQL wraps.
//
// Returns error which names the query and the column, or nil when every such column is
// wrapped.
func CheckColumnsReadable(emission *gogen.Emission, query *gogen.Query) error {
	columns := query.Analysed.OutputColumns
	for index := range columns {
		if !emission.NeedsJSONWrap(&columns[index]) {
			continue
		}
		if _, wrapped := query.Rendered.Marks.Wrapped[index]; wrapped {
			continue
		}
		return fmt.Errorf(
			"query %q: column %q %w (a compound UNION/INTERSECT/EXCEPT query, or a SELECT or RETURNING "+
				"projection item the generator cannot rewrite, such as an unaliased expression or a star "+
				"mixed with other items); rewrite it as an explicit aliased projection so the generator can "+
				"wrap it in %s() for typed scanning",
			query.Analysed.Name, columns[index].Name, errUnwrappableColumn, emission.Dialect().ToJSON)
	}
	return nil
}
