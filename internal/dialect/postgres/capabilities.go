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

import "seagull.sh/seagull/internal/dialect"

// The analyser finds a dialect's optional capabilities by type assertion, so these
// declarations have the compiler confirm that the PostgreSQL dialect still provides each
// one.
var (
	_ dialect.DialectPort = (*Dialect)(nil)

	_ dialect.OperatorTyperPort = (*Dialect)(nil)

	_ dialect.CatalogueFunctionResolverPort = (*Dialect)(nil)

	_ dialect.TableValuedFunctionTyperPort = (*Dialect)(nil)

	_ dialect.ElementNullabilityPort = (*Dialect)(nil)

	_ dialect.FunctionResolverPort = (*Dialect)(nil)

	_ dialect.RowValueTyperPort = (*Dialect)(nil)

	_ dialect.ArrayTyperPort = (*Dialect)(nil)

	_ dialect.ArgumentCoercerPort = (*Dialect)(nil)

	_ dialect.UntypedArgumentPort = (*Dialect)(nil)

	_ dialect.ExtensionLoaderPort = (*Dialect)(nil)
)
