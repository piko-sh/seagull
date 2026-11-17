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

// Package kinds declares named basic kinds a column override names.
package kinds

// Label is a named string kind.
type Label string

// Count is a named integer kind.
type Count int64

// Ratio is a named float kind.
type Ratio float64

// Flag is a named bool kind.
type Flag bool

// Blob is a named byte slice kind.
type Blob []byte
