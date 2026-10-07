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

package dialecttest

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// placeholderFormType is the type a typed placeholder form writes in the lexical
	// placeholder check.
	placeholderFormType = "Int32"

	// placeholderFormStatementFormat is the statement the lexical placeholder check writes
	// two placeholders into.
	placeholderFormStatementFormat = "SELECT id FROM items WHERE id = %s AND name = %s"
)

// placeholderForm is one way of writing a placeholder that Info describes, with the field
// that describes it.
type placeholderForm struct {
	// field names the Info field, for messages.
	field string

	// sigil is the form.
	sigil dialect.PlaceholderSigil
}

// checkLexicalReadsPlaceholderForms verifies that lexical rules recognise every declared
// placeholder form.
//
// Each must be read as one parameter token in both query source and generated SQL.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which holds every problem found.
func checkLexicalReadsPlaceholderForms(_ context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	info := fixture.NewDialect().Info()
	forms := placeholderForms(&info.Placeholders)
	problems := make([]string, 0, len(forms))
	for _, form := range forms {
		problems = append(problems, placeholderFormProblems(&info, form)...)
	}
	return outcomeOf(problems)
}

// placeholderForms lists the placeholder forms Info describes, each named after its
// field.
//
// Takes placeholders (*dialect.Placeholders) which describes the dialect's forms.
//
// Returns []placeholderForm which holds the forms that are set.
func placeholderForms(placeholders *dialect.Placeholders) []placeholderForm {
	forms := make([]placeholderForm, 0, len(placeholders.Sigils)+2)
	forms = append(forms, placeholderForm{field: "Info.Placeholders.Execution", sigil: placeholders.Execution})
	if placeholders.Positional.Prefix != "" {
		forms = append(forms, placeholderForm{field: "Info.Placeholders.Positional", sigil: placeholders.Positional})
	}
	for index, sigil := range placeholders.Sigils {
		forms = append(forms, placeholderForm{field: fmt.Sprintf("Info.Placeholders.Sigils[%d]", index), sigil: sigil})
	}
	return forms
}

// placeholderFormProblems writes two placeholders in one form into a statement and checks
// that the lexical rules read each as exactly one parameter token.
//
// Takes info (*dialect.Info) which describes the dialect.
// Takes form (placeholderForm) which is the form.
//
// Returns []string which describes each problem.
func placeholderFormProblems(info *dialect.Info, form placeholderForm) []string {
	first := form.sigil.Render(1, info.Placeholders.BoundName("first"), placeholderFormType)
	second := form.sigil.Render(2, info.Placeholders.BoundName("second"), placeholderFormType)
	statement := fmt.Sprintf(placeholderFormStatementFormat, first, second)
	tokens, err := significantTokens(info.Lexical, statement)
	if err != nil {
		return []string{fmt.Sprintf("%s: %q must read under Info.Lexical: %v", form.field, statement, err)}
	}
	var parameters []string
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			parameters = append(parameters, token.Raw)
		}
	}
	if len(parameters) != 2 || parameters[0] != first || parameters[1] != second {
		return []string{fmt.Sprintf("%s: Info.Lexical must read %q and %q as parameter tokens in %q, but reads [%s]",
			form.field, first, second, statement, strings.Join(parameters, ", "))}
	}
	return nil
}
