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

package app

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/fault"
)

// validateDialectInfo checks the metadata required for analysis and SQL generation.
//
// Missing required metadata is reported as a configuration error before it can produce
// misleading query diagnostics or invalid SQL.
//
// Takes info (*dialect.Info) which is the dialect's description.
//
// Returns error which wraps ErrInvalidConfig and names every missing field, or nil.
func validateDialectInfo(info *dialect.Info) error {
	problems := dialect.InfoProblems(info)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("dialect %q has an incomplete Info (%s): %w", info.Name, strings.Join(problems, "; "), fault.ErrInvalidConfig)
}
