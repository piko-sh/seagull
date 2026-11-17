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
	"cmp"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// recordViewReads remembers the tables and views a view's definition reads, so a later
// rename of one of them reaches the view.
//
// Takes view (*sqlmodel.View) which is the view just created.
// Takes definition (*ast.RawQueryAnalysis) which is its analysed definition, or nil.
func (b *catalogueBuilder) recordViewReads(view *sqlmodel.View, definition *ast.RawQueryAnalysis) {
	if definition == nil {
		return
	}

	reads, _ := b.relationsRead(definition)
	if b.viewReads == nil {
		b.viewReads = make(map[*sqlmodel.View]map[string]struct{})
	}
	b.viewReads[view] = reads
}

// viewsReading lists the views the catalogue still holds that read a relation of a name,
// ordered by schema and name so a refusal always names the same view.
//
// Takes key (string) which is the key of the relation's name.
//
// Returns []*sqlmodel.View which holds the views.
func (b *catalogueBuilder) viewsReading(key string) []*sqlmodel.View {
	var views []*sqlmodel.View
	for view, reads := range b.viewReads {
		if !b.holdsView(view) {
			delete(b.viewReads, view)
			continue
		}
		if _, reads := reads[key]; reads {
			views = append(views, view)
		}
	}
	slices.SortFunc(views, func(left, right *sqlmodel.View) int {
		return cmp.Or(cmp.Compare(left.Schema, right.Schema), cmp.Compare(left.Name, right.Name))
	})
	return views
}

// applyRenameToViews applies the engine's rule for the views that read a table being
// renamed. Depending on the engine, the views keep reading the table, the rename is
// refused, or the views break until a relation of the old name exists again.
//
// Takes oldName (string) which is the table's name.
// Takes newName (string) which is the name it is given.
//
// Returns error when the engine refuses to rename a table a view reads.
func (b *catalogueBuilder) applyRenameToViews(oldName, newName string) error {
	oldKey, newKey := b.key(oldName), b.key(newName)
	views := b.viewsReading(oldKey)
	switch b.engine.Info().ViewsOnRename {
	case dialect.ViewsRefuse:
		if len(views) > 0 {
			return fmt.Errorf("cannot rename table %s because view %s reads it", oldName, views[0].Name)
		}
	case dialect.ViewsBreak:
		for _, view := range views {
			view.Broken = fmt.Sprintf("view %s reads %s, which a migration renamed to %s, and this engine resolves a view's tables by name",
				view.Name, oldName, newName)
		}
	default:
		for _, view := range views {
			reads := b.viewReads[view]
			delete(reads, oldKey)
			reads[newKey] = struct{}{}
		}
	}
	return nil
}

// restoreBrokenViews mends each broken view once the catalogue again holds every relation
// its definition reads. An engine whose views name their tables resolves them as soon as
// a relation of the old name exists.
func (b *catalogueBuilder) restoreBrokenViews() {
	for view, reads := range b.viewReads {
		if view.Broken == "" || !b.holdsView(view) {
			continue
		}
		if b.holdsEveryRelation(reads) {
			view.Broken = ""
		}
	}
}

// holdsEveryRelation reports whether the catalogue holds a relation for every key.
//
// Takes keys (map[string]struct{}) which are the keys of the names.
//
// Returns bool which is true when none is missing.
func (b *catalogueBuilder) holdsEveryRelation(keys map[string]struct{}) bool {
	for key := range keys {
		if !b.holdsRelationKey(key) {
			return false
		}
	}
	return true
}

// holdsRelationKey reports whether any schema holds a table or a view under a name key.
//
// Takes key (string) which is the key of the name.
//
// Returns bool which is true when such a relation exists.
func (b *catalogueBuilder) holdsRelationKey(key string) bool {
	for _, schema := range b.catalogue.Schemas {
		if schema.Tables[key] != nil || schema.Views[key] != nil {
			return true
		}
	}
	return false
}

// applyDropToViews handles views that read a table being dropped.
//
// An engine that protects views refuses the drop unless CASCADE is given, which drops the
// views too. Otherwise the views break until a relation of the name exists again.
//
// Takes mutation (*ast.CatalogueMutation) which is the DROP TABLE.
//
// Returns error when the engine refuses to drop a table a view reads.
func (b *catalogueBuilder) applyDropToViews(mutation *ast.CatalogueMutation) error {
	views := b.viewsReading(b.key(mutation.TableName))
	if len(views) == 0 {
		return nil
	}
	if b.engine.Info().ViewsOnDrop != dialect.ViewsRefuse {
		for _, view := range views {
			view.Broken = fmt.Sprintf("view %s reads %s, which a migration dropped", view.Name, mutation.TableName)
		}
		return nil
	}
	if mutation.EngineSpecific[ast.EngineSpecificCascade] != "true" {
		return fmt.Errorf("cannot drop table %s because view %s reads it. Drop the view first or write CASCADE",
			mutation.TableName, views[0].Name)
	}
	b.dropViewsCascading(views)
	return nil
}

// dropViewsCascading drops views and, in turn, every view that reads one of them, as a
// DROP ... CASCADE does.
//
// Takes views ([]*sqlmodel.View) which are the views to drop.
func (b *catalogueBuilder) dropViewsCascading(views []*sqlmodel.View) {
	for len(views) > 0 {
		view := views[0]
		views = views[1:]
		if !b.holdsView(view) {
			continue
		}
		key := b.key(view.Name)
		delete(b.catalogue.Schemas[b.key(view.Schema)].Views, key)
		delete(b.viewReads, view)
		views = append(views, b.viewsReading(key)...)
	}
}
