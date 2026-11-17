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

//go:build ignore

package main

import (
	"context"
	generated "seagull.sh/compiletarget/generated"
)

// fuzzNewQueries creates queries that use the connection directly.
//
// Takes connection (generated.DBTX) which runs the generated queries.
//
// Returns *generated.Queries which uses the supplied connection.
// Returns func() error which is nil because no statements need closing.
// Returns error which is always nil.
func fuzzNewQueries(_ context.Context, connection generated.DBTX, _ []byte) (*generated.Queries, func() error, error) {
	return generated.New(connection), nil, nil
}

// fuzzTakePrepared returns no events for direct execution.
//
// Returns []preparedEvent which is always nil.
func fuzzTakePrepared() []preparedEvent { return nil }
