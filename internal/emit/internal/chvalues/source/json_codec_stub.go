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

import "encoding/json"

// sglJSONMarshal stands in for the generated package's JSON codec wrapper, which the
// shared helpers declare, so these helpers compile on their own.
//
// Takes value (any) which is the value to encode.
//
// Returns []byte which is the JSON text.
// Returns error when the value cannot be encoded.
func sglJSONMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

// sglJSONUnmarshal stands in for the generated package's JSON codec wrapper, which the
// shared helpers declare, so these helpers compile on their own.
//
// Takes data ([]byte) which is the JSON text.
// Takes value (any) which points to the value to fill.
//
// Returns error when the text is not one JSON document the value can hold.
func sglJSONUnmarshal(data []byte, value any) error {
	return json.Unmarshal(data, value)
}
