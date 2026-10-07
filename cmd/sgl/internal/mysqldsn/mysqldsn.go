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

// Package mysqldsn reads the one setting of a go-sql-driver/mysql data source name that
// the migration backend needs, without linking the driver, so the CLI stays free of
// driver packages for custom binaries.
package mysqldsn

import (
	"net/url"
	"strconv"
	"strings"
)

// multiStatementsParameter is the DSN parameter that lets the driver run several
// statements in one call.
const multiStatementsParameter = "multiStatements"

// MultiStatements reports whether a go-sql-driver/mysql data source name sets
// multiStatements to true.
//
// The parameters follow the first '?' after the last '/', as the driver reads them. They
// are parsed as a URL query, and the value is a boolean in any form strconv.ParseBool
// accepts. A DSN whose parameters cannot be parsed enables nothing, as the driver would
// refuse it anyway.
//
// Takes dsn (string) which is the data source name.
//
// Returns bool which is true when the DSN enables several statements per call.
func MultiStatements(dsn string) bool {
	_, database, hasSlash := strings.CutLast(dsn, "/")
	if !hasSlash {
		return false
	}
	_, query, hasParameters := strings.Cut(database, "?")
	if !hasParameters {
		return false
	}
	parameters, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	enabled, err := strconv.ParseBool(parameters.Get(multiStatementsParameter))
	return err == nil && enabled
}
