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

package panicguard_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/panicguard"
)

func TestErrorNamesTheOperationAndUnwrapsToErrPanicAndAnErrorValue(t *testing.T) {
	t.Parallel()
	failure := errors.New("disk on fire")
	cases := []struct {
		err         *panicguard.Error
		name        string
		wantMessage string
		wantUnwrap  []error
	}{
		{
			name:        "plain value",
			err:         &panicguard.Error{Value: "boom", Operation: "applying hook"},
			wantMessage: "applying hook: panic: boom",
			wantUnwrap:  []error{fault.ErrPanic},
		},
		{
			name:        "error value",
			err:         &panicguard.Error{Value: failure, Operation: "running dialect"},
			wantMessage: "running dialect: panic: disk on fire",
			wantUnwrap:  []error{fault.ErrPanic, failure},
		},
		{
			name:        "nil value",
			err:         &panicguard.Error{Value: nil, Operation: "observing"},
			wantMessage: "observing: panic: <nil>",
			wantUnwrap:  []error{fault.ErrPanic},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.wantMessage, testCase.err.Error())
			assert.Equal(t, testCase.wantUnwrap, testCase.err.Unwrap())
			for _, target := range testCase.wantUnwrap {
				assert.ErrorIs(t, testCase.err, target)
			}
		})
	}
}
