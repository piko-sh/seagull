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

package emit_test

import (
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/emit"
)

func TestEmitFacadeAPI(t *testing.T) {
	surface := apitest.Surface{
		"EmitterPort":           (*emit.EmitterPort)(nil),
		"TypeDefaulterPort":     (*emit.TypeDefaulterPort)(nil),
		"NullElementBinderPort": (*emit.NullElementBinderPort)(nil),
		"CheckerPort":           (*emit.CheckerPort)(nil),
		"ReserverPort":          (*emit.ReserverPort)(nil),

		"Request":        emit.Request{},
		"Style":          emit.Style{},
		"GeneratedFile":  emit.GeneratedFile{},
		"GoTypeResolver": (*emit.GoTypeResolver)(nil),
		"GoType":         emit.GoType{},
		"TypeSlot":       emit.TypeSlot{},
		"TypeSlots":      emit.TypeSlots{},

		"InlineJSONTypeName": emit.InlineJSONTypeName,

		"Naming":      emit.Naming{},
		"QueryNaming": emit.QueryNaming{},
		"ModelNaming": emit.ModelNaming{},
		"NamedType":   emit.NamedType{},
		"Reservation": emit.Reservation{},

		"AnalysedQuery":  emit.AnalysedQuery{},
		"QueryParameter": emit.QueryParameter{},
		"OutputColumn":   emit.OutputColumn{},
		"AllowedColumn":  emit.AllowedColumn{},
		"SQLTemplate":    emit.SQLTemplate{},
		"SQLSegment":     emit.SQLSegment{},

		"QueryCommand": emit.QueryCommand(0),

		"QueryCommandOne":        emit.QueryCommandOne,
		"QueryCommandMany":       emit.QueryCommandMany,
		"QueryCommandExec":       emit.QueryCommandExec,
		"QueryCommandExecResult": emit.QueryCommandExecResult,
		"QueryCommandExecRows":   emit.QueryCommandExecRows,
		"QueryCommandBatch":      emit.QueryCommandBatch,
		"QueryCommandStream":     emit.QueryCommandStream,
		"QueryCommandCopyFrom":   emit.QueryCommandCopyFrom,
		"QueryCommandAsyncExec":  emit.QueryCommandAsyncExec,

		"SQLSegmentKind": emit.SQLSegmentKind(0),

		"SegmentText":       emit.SegmentText,
		"SegmentParameter":  emit.SegmentParameter,
		"SegmentList":       emit.SegmentList,
		"SegmentColumn":     emit.SegmentColumn,
		"SegmentStar":       emit.SegmentStar,
		"SegmentFilter":     emit.SegmentFilter,
		"SegmentOrderBy":    emit.SegmentOrderBy,
		"SegmentPagination": emit.SegmentPagination,
		"SegmentValuesRow":  emit.SegmentValuesRow,

		"ParameterDirectiveKind": emit.ParameterDirectiveKind(0),

		"ParameterDirectiveNone":     emit.ParameterDirectiveNone,
		"ParameterDirectiveParam":    emit.ParameterDirectiveParam,
		"ParameterDirectiveSortable": emit.ParameterDirectiveSortable,

		"Problem":       emit.Problem{},
		"ErrorSeverity": emit.ErrorSeverity(0),

		"SeverityError":   emit.SeverityError,
		"SeverityWarning": emit.SeverityWarning,
		"SeverityHint":    emit.SeverityHint,

		"CodeEmitterUnsupported": emit.CodeEmitterUnsupported,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
