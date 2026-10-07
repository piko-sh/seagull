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

// Package emit_kit holds the building blocks for writing a code emitter: the tools the
// built-in emitters share, so that a new emitter renders SQL, checks queries and batches
// rows the way they do.
//
// An emitter implements emit.EmitterPort. Within Emit it takes the package's names from
// emit.Request.Naming, and renders each query's emit.SQLTemplate as the SQL its driver
// executes with [Render] and a [Renderer], which also gives the order the arguments bind
// in and the [Marks] the generated code edits at run time. [ExecutionForm] chooses the
// placeholder form the SQL executes in, and [CommentLine] writes the comment that names a
// query whose SQL another query writes alike.
//
// Its emit.CheckerPort refuses what the generated code could not serve with
// [CheckTypeDepth] and [CheckBatch], and [RowTemplate], [RowColumns] and
// [ClausesAfterValues] help it send the rows of a multi-row INSERT as a batch.
// [DefaultReservation] and [ReservationFor] give the names an emitter's code reserves.
// Every tool takes the dialect's facts, the Facts of sdk/dialect, from the request, so
// none branches on an engine.
//
// Check the finished emitter with emit_testkit.Run, the conformance suite every built-in
// emitter passes.
//
// # Usage
//
//	info := &request.Dialect
//	rendered, err := emit_kit.Render(query.Template, &emit_kit.Renderer{
//	    Query:        query,
//	    Quote:        info.IdentifierQuote,
//	    Lists:        info.Lists,
//	    Form:         emit_kit.ExecutionForm(info, query),
//	    Placeholders: info.Placeholders,
//	    ListMode:     emit_kit.ListExpand,
//	    KeepStars:    request.Style.KeepStars,
//	})
//	if err != nil {
//	    return nil, err
//	}
package emit_kit
