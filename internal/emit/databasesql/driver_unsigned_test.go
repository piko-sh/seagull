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

package databasesql

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBindsAsUnsigned(t *testing.T) {
	t.Run("only DuckDB converts a wide unsigned integer", func(t *testing.T) {
		parameter := &sqlmodel.QueryParameter{SQLType: sqlmodel.NewIntegerType("unsigned", 64, true)}
		for _, module := range []string{driverDuckDB, driverPgx, driverMySQL, driverClickHouse, driverModernc, driverMattn, ""} {
			profile := generalProfile
			if selected, ok := driverProfiles[module]; ok {
				profile = selected
			}
			for _, slice := range []bool{false, true} {
				parameter.IsSlice = slice
				assert.Equal(t, module == driverDuckDB, bindsAsUnsigned(&profile, parameter), "module %q slice %v", module, slice)
			}
		}
	})
	t.Run("narrower, wider and signed integers are not converted", func(t *testing.T) {
		profile := duckDBProfile()
		for _, sqlType := range []sqlmodel.SQLType{
			sqlmodel.NewIntegerType("signed", 64, false), sqlmodel.NewIntegerType("narrow", 32, true), sqlmodel.NewIntegerType("huge", 128, true),
		} {
			parameter := &sqlmodel.QueryParameter{SQLType: sqlType}
			assert.False(t, bindsAsUnsigned(&profile, parameter), "type %+v", sqlType)
		}
	})
}

func TestAnyParameterBindsAsUnsigned(t *testing.T) {
	duckDB := duckDBProfile()
	wide := &sqlmodel.AnalysedQuery{Parameters: []sqlmodel.QueryParameter{{SQLType: sqlmodel.NewIntegerType("unsigned", 64, true)}}}
	narrow := &sqlmodel.AnalysedQuery{Parameters: []sqlmodel.QueryParameter{{SQLType: sqlmodel.NewIntegerType("unsigned", 32, true)}}}
	runtime := &sqlmodel.AnalysedQuery{DynamicRuntime: true}
	assert.True(t, anyParameterBindsAsUnsigned(&duckDB, []*sqlmodel.AnalysedQuery{narrow, wide}))
	assert.True(t, anyParameterBindsAsUnsigned(&duckDB, []*sqlmodel.AnalysedQuery{runtime}))
	assert.False(t, anyParameterBindsAsUnsigned(&duckDB, []*sqlmodel.AnalysedQuery{narrow}))
	assert.False(t, anyParameterBindsAsUnsigned(&generalProfile, []*sqlmodel.AnalysedQuery{wide, runtime}))
}
