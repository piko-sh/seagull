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

// Package refusal turns the problems a built-in emitter's CheckerPort finds into the
// error its Emit returns, so a caller that emits without checking first still gets no
// code for a request the emitter refuses.
package refusal

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
)

// Error returns the error describing a check's error problems, or nil when there are
// none.
//
// Takes problems ([]emit.Problem) which are the problems the check found.
//
// Returns error which wraps emit.ErrRefused and names each error problem's query, code
// and message, leaving out a code the problem does not give, or nil.
func Error(problems []emit.Problem) error {
	var messages []string
	for _, problem := range problems {
		if problem.Severity != diag.SeverityError {
			continue
		}
		message := problem.Message
		if problem.Code != "" {
			message = problem.Code + " " + message
		}
		if problem.Query != nil {
			message = fmt.Sprintf("query %s: %s", problem.Query.Name, message)
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", emit.ErrRefused, strings.Join(messages, "; "))
}

// Invalid returns the error refusing a request that cannot be emitted as it is.
//
// Takes err (error) which says why the request cannot be emitted.
//
// Returns error which wraps emit.ErrRefused and err, or nil for a nil err.
func Invalid(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", emit.ErrRefused, err)
}
