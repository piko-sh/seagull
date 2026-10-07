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

package source

import (
	"fmt"
)

// sglWrapError returns an error a generated method met wrapped with a label naming the
// method, so errors.Is and errors.As still find the error beneath, or nil for a nil
// error.
//
// Takes label (string) which names the method after the error prefix.
// Takes err (error) which is the error the method met, or nil.
//
// Returns error which is the labelled error, or nil.
func sglWrapError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", label, err)
}

// sglWrapResult returns a function that hands back a call's value as it is and its error
// labelled as sglWrapError labels it, for a method that returns a call's two results.
//
// Takes label (string) which names the method.
//
// Returns func(T, error) (T, error) which passes the value through and labels the error.
func sglWrapResult[T any](label string) func(T, error) (T, error) {
	return func(value T, err error) (T, error) {
		return value, sglWrapError(label, err)
	}
}
