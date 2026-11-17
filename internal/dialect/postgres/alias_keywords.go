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

package postgres

import "seagull.sh/seagull/internal/dialect/dialectkit"

// postgresBareAliasRefusals lists the keywords PostgreSQL 18 refuses as an alias written
// straight after a select item, read by preparing SELECT 1 followed by each keyword. It
// takes every keyword after AS.
const postgresBareAliasRefusals = "ARRAY AS CHAR CHARACTER CREATE DAY EXCEPT FETCH FILTER FOR FROM GRANT GROUP HAVING HOUR " +
	"INTERSECT INTO LIMIT MINUTE MONTH OFFSET ON ORDER OVER OVERLAPS PRECISION RETURNING SECOND " +
	"TO UNION VARYING WHERE WINDOW WITH WITHIN WITHOUT YEAR"

// postgresAliasKeywords lists the keywords PostgreSQL refuses as an output column's
// alias.
var postgresAliasKeywords = dialectkit.NewAliasKeywords("", postgresBareAliasRefusals)
