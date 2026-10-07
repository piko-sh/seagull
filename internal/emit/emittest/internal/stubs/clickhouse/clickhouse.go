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

// Package clickhouse stands in for github.com/ClickHouse/clickhouse-go/v2 in the runtime
// lane of emittest: it declares Named, which generated native ClickHouse code binds every
// named parameter through.
package clickhouse

import "seagull.sh/seagull/internal/emit/emittest/internal/stubs/clickhouse/lib/driver"

// Named gives a query argument a name.
//
// Takes name (string) which is the parameter's name.
// Takes value (any) which is the value.
//
// Returns driver.NamedValue which binds the value under the name.
func Named(name string, value any) driver.NamedValue {
	return driver.NamedValue{Name: name, Value: value}
}
