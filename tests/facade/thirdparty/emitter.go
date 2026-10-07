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

package thirdparty

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/sdk/emit"
)

// Emitter generates Go for database/sql from analysed queries, using only Seagull's
// public packages.
type Emitter struct{}

var (
	_ emit.EmitterPort = (*Emitter)(nil)

	_ emit.ReserverPort = (*Emitter)(nil)

	_ emit.CheckerPort = (*Emitter)(nil)

	// errRefused reports a request the emitter refuses to emit.
	errRefused = errors.New("the thirdparty emitter refuses the request")
)

// New returns the emitter.
//
// Returns *Emitter which is ready to emit.
func New() *Emitter {
	return &Emitter{}
}

// Emit generates the package: a file per query file, the models, the querier, the helpers
// the queries need and the query-names lookup the style asks for.
//
// It refuses a request Check finds an error with, for a caller that emits without
// checking first. It stops early with the context's error when the context ends.
//
// Takes request (*emit.Request) which carries the queries, their names and the style.
//
// Returns []emit.GeneratedFile which holds the files.
// Returns error when Check refuses the request, a template does not render or a file does
// not format.
func (emitter *Emitter) Emit(ctx context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
	if err := refusal(emitter.Check(request)); err != nil {
		return nil, err
	}
	current, err := newGeneration(request)
	if err != nil {
		return nil, err
	}
	return current.files(ctx)
}

// refusal returns the error describing a check's error problems, or nil when there are
// none.
//
// Takes problems ([]emit.Problem) which are the problems Check found.
//
// Returns error which names each error problem, or nil.
func refusal(problems []emit.Problem) error {
	var messages []string
	for _, problem := range problems {
		if problem.Severity != emit.SeverityError {
			continue
		}
		message := problem.Code + " " + problem.Message
		if problem.Query != nil {
			message = "query " + problem.Query.Name + ": " + message
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", errRefused, strings.Join(messages, "; "))
}
