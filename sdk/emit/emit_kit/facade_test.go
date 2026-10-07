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

package emit_kit_test

import (
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

func TestEmitKitFacadeAPI(t *testing.T) {
	t.Parallel()

	surface := apitest.Surface{
		"ListExpand":     emit_kit.ListExpand,
		"ListArray":      emit_kit.ListArray,
		"ListAsWritten":  emit_kit.ListAsWritten,
		"ColumnWrapJSON": emit_kit.ColumnWrapJSON,
		"ColumnWrapText": emit_kit.ColumnWrapText,
		"BatchRows":      emit_kit.BatchRows,
		"BatchColumns":   emit_kit.BatchColumns,

		"Renderer":   emit_kit.Renderer{},
		"Rendered":   emit_kit.Rendered{},
		"Marks":      emit_kit.Marks{},
		"ColumnWrap": emit_kit.ColumnWrap{},
		"SegmentSet": emit_kit.SegmentSet(0),

		"NewSegmentSet": emit_kit.NewSegmentSet,

		"ListMode": emit_kit.ListMode(0),

		"ColumnWrapKind": emit_kit.ColumnWrapKind(0),

		"Row":       emit_kit.Row{},
		"BindLimit": emit_kit.BindLimit{},

		"BatchShape": emit_kit.BatchShape(0),

		"Render":             emit_kit.Render,
		"ExecutionForm":      emit_kit.ExecutionForm,
		"CommentLine":        emit_kit.CommentLine,
		"MethodName":         emit_kit.MethodName,
		"CheckBatch":         emit_kit.CheckBatch,
		"ClausesAfterValues": emit_kit.ClausesAfterValues,
		"RowColumns":         emit_kit.RowColumns,
		"RowTemplate":        emit_kit.RowTemplate,
		"CheckTypeDepth":     emit_kit.CheckTypeDepth,
		"DefaultReservation": emit_kit.DefaultReservation,
		"ReservationFor":     emit_kit.ReservationFor,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
