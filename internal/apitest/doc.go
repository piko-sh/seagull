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

// Package apitest records the exported surface of a facade package in a YAML golden file
// and fails a test when the surface drifts from it, so a change to a public API is always
// a reviewed diff rather than an accident.
//
// A facade test lists every exported name in a Surface, each mapped to a value that
// stands for it, and calls Check with the path of the golden file:
//
//	func TestFacadeAPI(t *testing.T) {
//		surface := apitest.Surface{
//			"Options":      Options{},
//			"New":          New,
//			"ObserverPort": (*ObserverPort)(nil),
//			"StageVet":     StageVet,
//		}
//		apitest.Check(t, surface, "facade_test.golden.yaml")
//	}
//
// A zero struct stands for a struct type, a nil pointer to an interface for the
// interface, and a function or a constant for itself. A generic member is listed with its
// type parameters instantiated.
//
// Check renders each entry through reflection, sorted by name and marked with its kind: a
// struct shows its exported fields, a count of its unexported ones and the methods of a
// pointer to it, an interface shows its methods, a function its signature and any other
// value its type.
//
// When a change to the surface is intended, record the golden file again and review the
// diff:
//
//	go test ./path -update-api
//
// The flag is named -update-api so it never collides with the -update flag a package
// registers for its own golden files. A test binary that imports apitest must not
// register -update-api itself.
//
// Check is safe to call from parallel tests that use different golden files. Test-only:
// no production code imports it.
package apitest
