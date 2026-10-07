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

// Package chcol provides the ClickHouse JSON interfaces used by the emitter runtime
// harness.
package chcol

import "encoding/json"

// JSON is a JSON document the driver decoded, its values by path.
type JSON struct {
	// valuesByPath holds the document's values, keyed by path.
	valuesByPath map[string]any
}

// MarshalJSON writes the document's values as one object.
//
// Returns []byte which is the object.
// Returns error when a value cannot be written.
func (document *JSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(document.valuesByPath)
}

// JSONDeserializer is a destination that reads a JSON column from the document the driver
// decoded.
type JSONDeserializer interface {
	// DeserializeClickHouseJSON reads the document.
	//
	// Takes document (*JSON) which is the document.
	//
	// Returns error when the document cannot be read.
	DeserializeClickHouseJSON(document *JSON) error
}
