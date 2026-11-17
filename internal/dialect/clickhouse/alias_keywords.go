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

package clickhouse

import "seagull.sh/seagull/internal/dialect/dialectkit"

// clickHouseBareAliasRefusals lists the keywords ClickHouse refuses as an alias written
// straight after a select item, found by planning SELECT 1 followed by each keyword.
// ClickHouse takes every keyword after AS.
const clickHouseBareAliasRefusals = "ALL AND ANTI ANY ARRAY AS ASOF BETWEEN CROSS DIV ELSE EXCEPT FINAL FORMAT FROM FULL GLOBAL " +
	"GROUP HAVING ILIKE IN INNER INTERSECT INTO JOIN LEFT LIKE LIMIT MOD NOT OFFSET ON ONLY OR " +
	"ORDER PARALLEL PASTE PREWHERE QUALIFY REGEXP RIGHT SAMPLE SEMI SETTINGS UNION USING WHERE " +
	"WINDOW WITH"

// clickHouseAliasKeywords lists the keywords ClickHouse refuses as an output column's
// alias.
var clickHouseAliasKeywords = dialectkit.NewAliasKeywords("", clickHouseBareAliasRefusals)
