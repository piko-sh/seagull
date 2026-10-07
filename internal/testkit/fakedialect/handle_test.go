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

package fakedialect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestHandleAndPortPassAnEngineThroughTheHandle(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}

	handle := fakedialect.Handle(engine)

	require.Equal(t, dialect.DialectPort(engine), fakedialect.Port(handle))
	require.Equal(t, "mock", handle.Info().Name)
	require.Zero(t, fakedialect.Handle(nil))
	require.Nil(t, fakedialect.Port(dialect.Dialect{}))
}

func TestRenamedReportsTheNewNameAndFamilyAndDefersTheRest(t *testing.T) {
	t.Parallel()

	normalised := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "whole")
	base := fakedialect.Handle(&fakedialect.Engine{
		NormaliseTypeNameFunc: func(string, ...int) sqlmodel.SQLType { return normalised },
	})

	renamed := fakedialect.Renamed(base, "acme", "acmes")

	facts, baseFacts := renamed.Info(), base.Info()
	require.Equal(t, "acme", facts.Name)
	require.Equal(t, "acmes", facts.Family)
	facts.Name, facts.Family = baseFacts.Name, baseFacts.Family
	require.Equal(t, baseFacts, facts)
	require.Equal(t, normalised, renamed.NormaliseTypeName("anything"))
	require.Equal(t, "mock", base.Info().Name, "the wrapped engine keeps its own name")
}

func TestInfoPanicsPanicsWithTheValueAndDefersTheRest(t *testing.T) {
	t.Parallel()

	normalised := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "words")
	base := fakedialect.Handle(&fakedialect.Engine{
		NormaliseTypeNameFunc: func(string, ...int) sqlmodel.SQLType { return normalised },
	})

	broken := fakedialect.InfoPanics(base, "no info")

	require.PanicsWithValue(t, "no info", func() { broken.Info() })
	require.Equal(t, normalised, broken.NormaliseTypeName("anything"))
}
