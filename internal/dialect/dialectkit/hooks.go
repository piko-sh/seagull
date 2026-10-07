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

package dialectkit

import "slices"

// FirstHookAnswer asks a dialect's hooks, most recently registered first, for an answer,
// and returns the first one a hook gives.
//
// A hook declines by returning nil. A panic in a hook is not recovered here: it goes on
// to the guard where Seagull called the dialect, which reports it once.
//
// Takes hooks ([]H) which holds the hooks in registration order.
// Takes ask (func(hook H) *R) which calls one hook and returns its answer, or nil when it
// declines.
//
// Returns R which is the first answer, or the zero value when every hook declines.
// Returns bool which is true when a hook answered.
func FirstHookAnswer[H any, R any](hooks []H, ask func(hook H) *R) (R, bool) {
	for _, hook := range slices.Backward(hooks) {
		if answer := ask(hook); answer != nil {
			return *answer, true
		}
	}
	var zero R
	return zero, false
}
