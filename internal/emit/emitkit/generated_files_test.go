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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
)

func TestReservedGeneratedFiles(t *testing.T) {
	reservation := emitkit.DefaultReservation(nil, &emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"})
	require.ElementsMatch(t, []string{
		"querier.go",
		"querier_interface.go",
		"models.go",
		"prepared.go",
		"sortable.go",
		"bind_limits.go",
		"slice_helpers.go",
		"runtime_helpers.go",
		"sgl_json.go",
		"scan_numbers.go",
		"array_literals.go",
		"required_slices.go",
		"scan_arrays.go",
		"sgl_null.go",
		"sgl_null_pgx.go",
		"sgl_json_codec.go",
		"clickhouse_batches.go",
		"duckdb_appending.go",
		"driver_values.go",
		"driver_composites.go",
		"clickhouse_values.go",
		"clickhouse_appending.go",
		"clickhouse_composites.go",
		"clickhouse_documents.go",
		"value_reading.go",
		"error_wrapping.go",
		"convert_strings.go",
	}, reservation.Files)
}
