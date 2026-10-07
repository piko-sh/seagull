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
	"reflect"
)

// ReadList reads a list into a slice as the rendered sglReadList does, for the stand-ins
// of the helper sources rendered beside this one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one element.
//
// Returns error when the source cannot be represented by the destination.
func ReadList(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	return sglReadList(target, value, read)
}

// ReadMap reads a map into a map as the rendered sglReadMap does, for the stand-ins of
// the helper sources rendered beside this one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one key or value.
//
// Returns error when the source cannot be represented by the destination.
func ReadMap(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	return sglReadMap(target, value, read)
}

// ReadScalar converts within scalar kinds as the rendered sglReadScalar does, for the
// stand-ins of the helper sources rendered beside this one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
func ReadScalar(target reflect.Value, value reflect.Value) error {
	return sglReadScalar(target, value)
}

// Unreadable returns the error the rendered sglUnreadable returns, for the stand-ins of
// the helper sources rendered beside this one.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns error which describes the unsupported source and destination types.
func Unreadable(source reflect.Type, target reflect.Type) error {
	return sglUnreadable(source, target)
}
