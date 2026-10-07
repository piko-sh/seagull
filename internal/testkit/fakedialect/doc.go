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

// Package fakedialect provides programmable stand-ins for a dialect engine and a code
// emitter, plus small catalogue builders, for the analyser's tests, and the helpers that
// pass an engine to the facades as a handle: Handle and Port, and Renamed and InfoPanics,
// which wrap a built-in engine.
//
// Every port method calls the matching function field when it is set and otherwise
// returns a neutral default, so a test overrides only the behaviour it exercises. The
// package is imported only from tests.
package fakedialect
