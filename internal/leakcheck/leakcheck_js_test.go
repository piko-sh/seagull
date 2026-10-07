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

package leakcheck

import (
	"syscall/js"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestTheSharedRulesIgnoreTheEventHandlerOfACallbackTheHostCalled(t *testing.T) {
	called := make(chan struct{})
	callback := js.FuncOf(func(js.Value, []js.Value) any {
		close(called)
		return nil
	})
	defer callback.Release()
	js.Global().Call("setTimeout", callback, 0)
	<-called

	testCases := []struct {
		name         string
		find         func() error
		expectedLeak bool
	}{
		{name: "the shared rules ignore the event handler", find: func() error { return FindLeaks() }, expectedLeak: false},
		{name: "goleak alone reports it", find: func() error { return goleak.Find() }, expectedLeak: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.find()

			if testCase.expectedLeak {
				require.ErrorContains(t, err, eventHandlerFunction)
				return
			}
			require.NoError(t, err)
		})
	}
}
