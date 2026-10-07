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

package methods

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestGroupByKeyColumnAndTableSplitOnDot(t *testing.T) {
	dotted := &sqlmodel.AnalysedQuery{GroupByKey: []string{"orders.id"}}
	assert.Equal(t, "id", groupByKeyColumn(dotted))
	assert.Equal(t, "orders", groupByKeyTable(dotted))

	bare := &sqlmodel.AnalysedQuery{GroupByKey: []string{"id"}}
	assert.Equal(t, "id", groupByKeyColumn(bare))
	assert.Equal(t, "", groupByKeyTable(bare))

	none := &sqlmodel.AnalysedQuery{}
	assert.Equal(t, "", groupByKeyColumn(none))
	assert.Equal(t, "", groupByKeyTable(none))
}

func TestEmbedGroupsFollowThePlanAndTheColumns(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:     "ListReports",
		Filename: "reports.sql",
		Command:  sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			namedEmbeddedColumn("id", "employees", "Report", false),
			namedEmbeddedColumn("id", "employees", "Manager", true),
			namedEmbeddedColumn("name", "employees", "Manager", true),
		},
		Template: template(text("SELECT r.id, r.id, m.id, m.name FROM employees r LEFT JOIN employees m ON m.id = r.manager")),
	})

	groups := embedGroups(query)

	require.Equal(t, []int{0}, flatColumns(query.Analysed))
	require.Len(t, groups, 2)
	assert.Equal(t, "ListReportsReport", groups[0].naming.Type)
	assert.Equal(t, "employees", groups[0].TableName)
	assert.Equal(t, []int{1}, groups[0].Columns)
	assert.False(t, groups[0].IsOuter)
	assert.Equal(t, "Manager", groups[1].naming.Field)
	assert.Equal(t, []int{2, 3}, groups[1].Columns)
	assert.True(t, groups[1].IsOuter)
}

func TestDetailEmbedIsAnyTableButTheKey(t *testing.T) {
	keyGroup := embedGroup{TableName: "orders"}
	detailGroup := embedGroup{TableName: "items"}
	assert.False(t, isGroupByDetailEmbed(&keyGroup, "orders"), "the key table is not a detail embed")
	assert.True(t, isGroupByDetailEmbed(&detailGroup, "orders"), "a non-key table is a detail embed")
	assert.False(t, isGroupByDetailEmbed(&detailGroup, ""), "no key table means no detail embeds")
}

func TestGroupedLocalsComeFromThePlan(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:       "ListOrders",
		Filename:   "orders.sql",
		Command:    sqlmodel.QueryCommandMany,
		GroupByKey: []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			textColumn("id", false),
			embeddedColumn("sku", "items", true),
		},
		Template: template(text("SELECT orders.id, items.order_id AS id, items.sku FROM orders LEFT JOIN items ON items.order_id = orders.id")),
	})

	method := GroupedManyMethod(emission, query, StaticPrelude(query))

	source := renderDecl(t, method)
	require.Contains(t, source, "var colID string")
	require.Contains(t, source, "var colID2 string")
	require.Contains(t, source, "rows.Scan(&colID, &colID2, &itemsSku)")
}

func embeddedColumn(name, embedTable string, isOuter bool) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{
		Name:         name,
		SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		IsEmbedded:   true,
		EmbedTable:   embedTable,
		EmbedIsOuter: isOuter,

		Nullable: isOuter,
	}
}

func namedEmbeddedColumn(name, embedTable, embedName string, isOuter bool) sqlmodel.OutputColumn {
	column := embeddedColumn(name, embedTable, isOuter)
	column.EmbedName = embedName
	return column
}
