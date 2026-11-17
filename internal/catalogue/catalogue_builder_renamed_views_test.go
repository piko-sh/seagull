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

package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestViewsReadingListsTheViewsInOrderAndForgetsDroppedOnes(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	views := builder.catalogue.Schemas["public"].Views
	reads := func() map[string]struct{} { return map[string]struct{}{"items": {}} }
	builder.viewReads = make(map[*sqlmodel.View]map[string]struct{})
	for _, name := range []string{"zebra", "apple", "mango"} {
		view := &sqlmodel.View{Name: name, Schema: "public"}
		views[name] = view
		builder.viewReads[view] = reads()
	}
	dropped := &sqlmodel.View{Name: "gone", Schema: "public"}
	builder.viewReads[dropped] = reads()

	reading := builder.viewsReading("items")

	names := make([]string, 0, len(reading))
	for _, view := range reading {
		names = append(names, view.Name)
	}
	require.Equal(t, []string{"apple", "mango", "zebra"}, names)
	require.NotContains(t, builder.viewReads, dropped)
}

func TestRestoreBrokenViewsMendsAViewOnceEveryRelationIsBack(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	view := &sqlmodel.View{Name: "listing", Schema: "public", Broken: "broken"}
	builder.catalogue.Schemas["public"].Views["listing"] = view
	builder.viewReads = map[*sqlmodel.View]map[string]struct{}{view: {"items": {}, "owners": {}}}

	builder.restoreBrokenViews()
	require.Equal(t, "broken", view.Broken, "owners is still missing")

	builder.catalogue.Schemas["public"].Tables["owners"] = &sqlmodel.Table{Name: "owners", Schema: "public"}
	builder.restoreBrokenViews()
	require.Empty(t, view.Broken)
}

func builderWithReadingViews(t *testing.T, rule dialect.ViewDependencyRule) *catalogueBuilder {
	t.Helper()

	builder, engine := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	info := engine.Info()
	info.ViewsOnRename, info.ViewsOnDrop = rule, rule
	engine.InfoFunc = func() dialect.Info { return info }
	views := builder.catalogue.Schemas["public"].Views
	builder.viewReads = make(map[*sqlmodel.View]map[string]struct{})
	for name, reads := range map[string]string{"second": "items", "first": "items", "nested": "first"} {
		view := &sqlmodel.View{Name: name, Schema: "public"}
		views[name] = view
		builder.viewReads[view] = map[string]struct{}{reads: {}}
	}
	return builder
}

func TestRenamingATableAViewReadsFollowsTheEngineRule(t *testing.T) {
	t.Parallel()

	t.Run("refuse names the first view", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsRefuse)

		err := builder.applyRenameToViews("items", "goods")

		require.ErrorContains(t, err, "view first reads it")
	})
	t.Run("break marks every reading view", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsBreak)

		require.NoError(t, builder.applyRenameToViews("items", "goods"))

		views := builder.catalogue.Schemas["public"].Views
		require.NotEmpty(t, views["first"].Broken)
		require.NotEmpty(t, views["second"].Broken)
		require.Empty(t, views["nested"].Broken)
	})
	t.Run("follow moves the reads to the new name", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsFollow)

		require.NoError(t, builder.applyRenameToViews("items", "goods"))

		require.Empty(t, builder.viewsReading("items"))
		require.Len(t, builder.viewsReading("goods"), 2)
	})
}

func TestDroppingATableAViewReadsFollowsTheEngineRule(t *testing.T) {
	t.Parallel()

	drop := func(cascade bool) *ast.CatalogueMutation {
		options := []ast.CatalogueMutationOption{}
		if cascade {
			options = append(options, ast.WithEngineSpecific(map[string]string{ast.EngineSpecificCascade: "true"}))
		}
		return ast.NewCatalogueMutation(ast.MutationDropTable, "public", "items", options...)
	}

	t.Run("refuse without CASCADE", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsRefuse)

		require.ErrorContains(t, builder.applyDropToViews(drop(false)), "view first reads it")
	})
	t.Run("CASCADE drops the reading views and the views reading them", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsRefuse)

		require.NoError(t, builder.applyDropToViews(drop(true)))

		require.Empty(t, builder.catalogue.Schemas["public"].Views)
		require.Empty(t, builder.viewReads)
	})
	t.Run("break marks the reading views", func(t *testing.T) {
		t.Parallel()

		builder := builderWithReadingViews(t, dialect.ViewsBreak)

		require.NoError(t, builder.applyDropToViews(drop(false)))

		require.NotEmpty(t, builder.catalogue.Schemas["public"].Views["first"].Broken)
	})
}
