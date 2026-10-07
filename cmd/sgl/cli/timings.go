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

package cli

import (
	"fmt"
	"io"
	"time"

	"seagull.sh/seagull/cmd/internal/assemble"
)

// writeTimings prints the stage table of generate --timings: one row per stage, in the
// order the stages first started, with its span count, total time and failed spans.
//
// Takes writer (io.Writer) which receives the table.
// Takes database (string) which names the database the run was for.
// Takes stages ([]assemble.StageTiming) which holds the run's stage timings.
func writeTimings(writer io.Writer, database string, stages []assemble.StageTiming) {
	fmt.Fprintf(writer, "timings for database %q:\n", database)
	table := newTable(writer)
	fmt.Fprintln(table, "  STAGE\tSPANS\tTOTAL\tFAILED")
	for _, timing := range stages {
		fmt.Fprintf(table, "  %s\t%d\t%s\t%d\n", timing.Stage, timing.Spans, timing.Total.Round(time.Microsecond), timing.Failures)
	}
	_ = table.Flush()
}
