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

package helpers

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

const (
	// directivePrefix starts every directive of a helper source.
	directivePrefix = "//sgl:"

	// nolintPrefix starts a linter directive of a helper source, which the rendered file
	// leaves out.
	nolintPrefix = "//nolint:"

	// directiveWhen keeps the declaration below only when its flags hold.
	directiveWhen = "when"

	// directiveData replaces the literal the declaration below holds.
	directiveData = "data"

	// directiveInline removes the declaration below and writes its value where it is used.
	directiveInline = "inline"

	// emptyComment is the empty comment line gofmt writes between a doc comment's text and
	// its directives.
	emptyComment = "//"

	// negation marks a flag of a when directive that holds when the flag does not.
	negation = "!"
)

// directives holds the directives written above one declaration.
type directives struct {
	// data is the key of the declaration's data directive, or empty.
	data string

	// inline is the key of the declaration's inline directive, or empty.
	inline string

	// when lists the flags of the declaration's when directives.
	when []string
}

// read records one directive.
//
// Takes body (string) which is the directive without its prefix, such as "when flag".
//
// Returns error when the directive is unknown or malformed.
func (found *directives) read(body string) error {
	fields := strings.Fields(body)
	if len(fields) < 2 {
		return fmt.Errorf("%w: directive %q needs an argument", errInvalidSource, directivePrefix+body)
	}
	switch fields[0] {
	case directiveWhen:
		found.when = append(found.when, fields[1:]...)
	case directiveData:
		found.data = fields[1]
	case directiveInline:
		found.inline = fields[1]
	default:
		return fmt.Errorf("%w: unknown directive %q", errInvalidSource, directivePrefix+body)
	}
	if fields[0] != directiveWhen && len(fields) > 2 {
		return fmt.Errorf("%w: directive %q takes one key", errInvalidSource, directivePrefix+body)
	}
	return nil
}

// holds reports whether every flag of a when directive holds.
//
// Takes flags ([]string) which lists the flags, each maybe negated.
//
// Returns bool which is true when every flag holds.
// Returns error when the data holds no value for a flag.
func (r *renderer) holds(flags []string) (bool, error) {
	for _, flag := range flags {
		name, negated := strings.CutPrefix(flag, negation)
		value, known := r.data.Flags[name]
		if !known {
			return false, fmt.Errorf("%w: flag %q", errMissingData, name)
		}
		if value == negated {
			return false, nil
		}
	}
	return true, nil
}

// planDeclarations evaluates the directives of every top-level declaration and of every
// spec of a grouped one: it removes the declarations and specs whose flags do not hold
// and the inlined ones, and replaces the literals of the data ones.
//
// Returns map[*ast.Comment]bool which records every directive comment attached to a
// declaration or spec.
// Returns error when a directive is malformed or names data the data does not hold.
func (r *renderer) planDeclarations() (map[*ast.Comment]bool, error) {
	attached := make(map[*ast.Comment]bool)
	for _, declaration := range r.file.Decls {
		if err := r.planDeclaration(declaration, attached); err != nil {
			return nil, err
		}
	}
	return attached, nil
}

// planDeclaration evaluates the directives of one top-level declaration, then those of
// its specs when it is a grouped declaration that is kept as it is.
//
// Takes declaration (ast.Decl) which is the declaration.
// Takes attached (map[*ast.Comment]bool) which records each directive comment read.
//
// Returns error when a directive is malformed or names data the data does not hold.
func (r *renderer) planDeclaration(declaration ast.Decl, attached map[*ast.Comment]bool) error {
	doc := declarationDoc(declaration)
	found, err := parseDirectives(doc, attached)
	if err != nil {
		return err
	}
	specs, err := parseSpecDirectives(declaration, attached)
	if err != nil {
		return err
	}
	keep, err := r.holds(found.when)
	if err != nil {
		return err
	}
	switch {
	case !keep:
		r.removeDeclaration(declaration, doc)
		return nil
	case specs != nil && (found.inline != "" || found.data != ""):
		return fmt.Errorf("%w: a declaration that is inline or data has specs with directives of their own", errInvalidSource)
	case found.inline != "":
		return r.inlineDeclaration(declaration, doc, found.inline)
	case found.data != "":
		return r.substituteDeclaration(declaration, found.data)
	case specs != nil:
		if general, ok := declaration.(*ast.GenDecl); ok {
			return r.planSpecs(general, doc, specs)
		}
		return nil
	default:
		return nil
	}
}

// planSpecs evaluates the directives of the specs of a grouped declaration: it removes
// the specs whose flags do not hold and the inlined ones, or the whole declaration when
// none is left, and replaces the literals of the data ones.
//
// Takes general (*ast.GenDecl) which is the grouped declaration.
// Takes doc (*ast.CommentGroup) which is its doc comment, or nil.
// Takes found ([]directives) which holds the directives of each of its specs.
//
// Returns error when a directive names data the data does not hold or a spec does not
// suit its directive.
func (r *renderer) planSpecs(general *ast.GenDecl, doc *ast.CommentGroup, found []directives) error {
	removed := make([]bool, len(general.Specs))
	for index, spec := range general.Specs {
		keep, err := r.holds(found[index].when)
		if err != nil {
			return err
		}
		switch {
		case !keep:
			removed[index] = true
		case found[index].inline != "":
			removed[index] = true
			err = r.inlineSpec(spec, found[index].inline)
		case found[index].data != "":
			err = r.substituteSpec(spec, found[index].data)
		}
		if err != nil {
			return err
		}
	}
	if !slices.Contains(removed, false) {
		r.removeDeclaration(general, doc)
		return nil
	}
	for first := 0; first < len(removed); first++ {
		if !removed[first] {
			continue
		}
		last := first
		for last+1 < len(removed) && removed[last+1] {
			last++
		}
		r.removeSpecs(general.Specs, first, last)
		first = last
	}
	return nil
}

// removeSpecs removes a run of specs of a grouped declaration, with their doc comments
// and the blank line that parts the run from the specs left.
//
// The specs left then read as if the run had never been written. A run that opens the
// block goes with the lines below it up to the next spec; any other goes with the lines
// above it from the spec before.
//
// Takes specs ([]ast.Spec) which holds the declaration's specs, of which at least one
// stays.
// Takes first (int) which is the index of the run's first spec.
// Takes last (int) which is the index of the run's last spec.
func (r *renderer) removeSpecs(specs []ast.Spec, first, last int) {
	if first == 0 {
		from := r.lineStart(r.offset(specStart(specs[first])))
		to := r.lineStart(r.offset(specStart(specs[last+1])))
		r.add(edit{start: from, end: to, replacement: "", expand: nil})
		return
	}
	from := r.lineEnd(r.offset(specs[first-1].End()) - 1)
	to := r.lineEnd(r.offset(specs[last].End()) - 1)
	r.add(edit{start: from, end: to, replacement: "", expand: nil})
}

// removeDeclaration removes the lines of a declaration and its doc comment.
//
// Takes declaration (ast.Decl) which is the declaration.
// Takes doc (*ast.CommentGroup) which is its doc comment, or nil.
func (r *renderer) removeDeclaration(declaration ast.Decl, doc *ast.CommentGroup) {
	start := declaration.Pos()
	if doc != nil {
		start = doc.Pos()
	}
	from, to := r.wholeLines(start, declaration.End())
	r.add(edit{start: from, end: to, replacement: "", expand: nil})
}

// inlineDeclaration removes an inlined declaration and records its value, which is
// written wherever its name is used.
//
// Takes declaration (ast.Decl) which is the declaration.
// Takes doc (*ast.CommentGroup) which is its doc comment.
// Takes key (string) which names its value in the data.
//
// Returns error when the declaration does not declare one name or the data holds no value
// for the key.
func (r *renderer) inlineDeclaration(declaration ast.Decl, doc *ast.CommentGroup, key string) error {
	if err := r.inlineSpec(singleSpec(declaration), key); err != nil {
		return err
	}
	r.removeDeclaration(declaration, doc)
	return nil
}

// inlineSpec records the value of an inlined spec, which is written wherever its name is
// used; the caller removes the spec.
//
// Takes spec (ast.Spec) which is the spec, or nil when the declaration has more than one.
// Takes key (string) which names its value in the data.
//
// Returns error when the spec does not declare one name or the data holds no value for
// the key.
func (r *renderer) inlineSpec(spec ast.Spec, key string) error {
	name, ok := specName(spec)
	if !ok {
		return fmt.Errorf("%w: inline %q must declare one name", errInvalidSource, key)
	}
	value, known := r.data.Values[key]
	if !known {
		return fmt.Errorf("%w: value %q", errMissingData, key)
	}
	r.inlined[name] = value
	return nil
}

// substituteDeclaration replaces the literal a data declaration holds with its value.
//
// Takes declaration (ast.Decl) which is the declaration.
// Takes key (string) which names its value in the data.
//
// Returns error when the declaration does not hold one value or the data holds no value
// for the key.
func (r *renderer) substituteDeclaration(declaration ast.Decl, key string) error {
	return r.substituteSpec(singleSpec(declaration), key)
}

// substituteSpec replaces the literal a data spec holds with its value: the declared
// value itself, or its sole argument when the value is a call such as
// regexp.MustCompile(`...`).
//
// Takes spec (ast.Spec) which is the spec, or nil when the declaration has more than one.
// Takes key (string) which names its value in the data.
//
// Returns error when the spec does not hold one value or the data holds no value for the
// key.
func (r *renderer) substituteSpec(spec ast.Spec, key string) error {
	target, ok := dataTarget(spec)
	if !ok {
		return fmt.Errorf("%w: data %q must declare one value", errInvalidSource, key)
	}
	value, known := r.data.Values[key]
	if !known {
		return fmt.Errorf("%w: value %q", errMissingData, key)
	}
	literal, isLiteral := target.(*ast.BasicLit)
	raw := isLiteral && strings.HasPrefix(literal.Value, "`")
	text, err := r.literal(value, raw)
	if err != nil {
		return fmt.Errorf("data %q: %w", key, err)
	}
	r.add(edit{start: r.offset(target.Pos()), end: r.offset(target.End()), replacement: text, expand: nil})
	return nil
}

// planComments removes the directive and nolint lines, and the comments that document
// declarations, specs and fields when the vocabulary leaves them out, and renames the
// identifiers the remaining comments mention.
//
// Takes attached (map[*ast.Comment]bool) which records the directives read from
// declarations.
//
// Returns error when a directive is not attached to a top-level declaration or a spec of
// a grouped one.
func (r *renderer) planComments(attached map[*ast.Comment]bool) error {
	undocumented := r.undocumentedGroups()
	for _, group := range r.file.Comments {
		if undocumented[group] {
			if err := r.removeDocumentation(group, attached); err != nil {
				return err
			}
			continue
		}
		for index := range group.List {
			if err := r.planComment(group.List, index, attached); err != nil {
				return err
			}
		}
	}
	return nil
}

// planComment removes a directive or nolint comment, or renames the identifiers any other
// comment mentions.
//
// Takes comments ([]*ast.Comment) which holds the comment group the comment is in.
// Takes index (int) which is the comment's index in it.
// Takes attached (map[*ast.Comment]bool) which records the directives read from
// declarations.
//
// Returns error when a directive is not attached to a top-level declaration or a spec of
// a grouped one.
func (r *renderer) planComment(comments []*ast.Comment, index int, attached map[*ast.Comment]bool) error {
	comment := comments[index]
	switch {
	case strings.HasPrefix(comment.Text, directivePrefix):
		if err := r.requireAttached(comment, attached); err != nil {
			return err
		}
		r.removeDirective(comments, index)
	case strings.HasPrefix(comment.Text, nolintPrefix):
		r.removeDirective(comments, index)
	default:
		if renamed := r.renameText(comment.Text); renamed != comment.Text {
			r.add(edit{start: r.offset(comment.Pos()), end: r.offset(comment.End()), replacement: renamed, expand: nil})
		}
	}
	return nil
}

// requireAttached checks that a directive was read from a top-level declaration or a spec
// of a grouped one.
//
// Takes comment (*ast.Comment) which is the directive.
// Takes attached (map[*ast.Comment]bool) which records the directives read from
// declarations and specs.
//
// Returns error when the directive is not above a top-level declaration or a spec of a
// grouped one.
func (r *renderer) requireAttached(comment *ast.Comment, attached map[*ast.Comment]bool) error {
	if attached[comment] {
		return nil
	}
	return fmt.Errorf("%w: directive %q at %s is not above a top-level declaration or a spec of a grouped one",
		errInvalidSource, comment.Text, r.fileSet.Position(comment.Pos()))
}

// undocumentedGroups returns the comment groups that document a declaration, a spec or a
// field when the vocabulary leaves doc comments out of the rendered file.
//
// Returns map[*ast.CommentGroup]bool which holds those groups, and is empty when the
// rendered file keeps its doc comments.
func (r *renderer) undocumentedGroups() map[*ast.CommentGroup]bool {
	groups := make(map[*ast.CommentGroup]bool)
	if r.vocabulary == nil || r.vocabulary.DocComments() {
		return groups
	}
	for node := range ast.Preorder(r.file) {
		for _, group := range documentation(node) {
			if group != nil {
				groups[group] = true
			}
		}
	}
	return groups
}

// removeDocumentation removes a comment group that documents a declaration, a spec or a
// field, together with the blank line a field or spec list keeps above it, so the
// remaining list is laid out as if it had never been documented.
//
// Takes group (*ast.CommentGroup) which is the documentation.
// Takes attached (map[*ast.Comment]bool) which records the directives read from
// declarations.
//
// Returns error when a directive in the group is not above a top-level declaration or a
// spec of a grouped one.
func (r *renderer) removeDocumentation(group *ast.CommentGroup, attached map[*ast.Comment]bool) error {
	for _, comment := range group.List {
		if strings.HasPrefix(comment.Text, directivePrefix) {
			if err := r.requireAttached(comment, attached); err != nil {
				return err
			}
		}
		r.removeComment(comment)
	}
	r.removeBlankLineAbove(group)
	return nil
}

// removeBlankLineAbove removes the blank line above a comment group that stands on its
// own lines inside a declaration, such as a field's doc comment. Blank lines between
// top-level declarations are left to the spacing finish applies.
//
// Takes group (*ast.CommentGroup) which is the removed comment group.
func (r *renderer) removeBlankLineAbove(group *ast.CommentGroup) {
	start := r.offset(group.Pos())
	lineStart := r.lineStart(start)
	if lineStart == start || lineStart == 0 || strings.TrimSpace(string(r.source[lineStart:start])) != "" {
		return
	}
	previousStart := r.lineStart(lineStart - 1)
	if strings.TrimSpace(string(r.source[previousStart:lineStart])) != "" {
		return
	}
	r.add(edit{start: previousStart, end: lineStart, replacement: "", expand: nil})
}

// removeDirective removes a directive comment and, when it is the first directive of a
// doc comment, the empty comment line gofmt writes between the doc text and its
// directives.
//
// Takes comments ([]*ast.Comment) which holds the comment group the directive is in.
// Takes index (int) which is the directive's index in it.
func (r *renderer) removeDirective(comments []*ast.Comment, index int) {
	r.removeComment(comments[index])
	if index == 0 || comments[index-1].Text != emptyComment {
		return
	}
	if index >= 2 && isDirective(comments[index-2].Text) {
		return
	}
	r.removeComment(comments[index-1])
}

// removeComment removes a comment: its whole line when it stands alone on it, or the
// comment and the space before it when it follows code.
//
// Takes comment (*ast.Comment) which is the comment.
func (r *renderer) removeComment(comment *ast.Comment) {
	start := r.offset(comment.Pos())
	lineStart := r.lineStart(start)
	if strings.TrimSpace(string(r.source[lineStart:start])) == "" {
		r.add(edit{start: lineStart, end: r.lineEnd(start), replacement: "", expand: nil})
		return
	}
	trimmed := len(strings.TrimRight(string(r.source[:start]), " \t"))
	r.add(edit{start: trimmed, end: r.offset(comment.End()), replacement: "", expand: nil})
}

// parseDirectives reads the directives of a declaration's doc comment.
//
// Takes doc (*ast.CommentGroup) which is the doc comment, or nil.
// Takes attached (map[*ast.Comment]bool) which records each directive comment read.
//
// Returns directives which holds the directives.
// Returns error when a directive is unknown or malformed.
func parseDirectives(doc *ast.CommentGroup, attached map[*ast.Comment]bool) (directives, error) {
	found := directives{data: "", inline: "", when: nil}
	if doc == nil {
		return found, nil
	}
	for _, comment := range doc.List {
		body, isDirective := strings.CutPrefix(comment.Text, directivePrefix)
		if !isDirective {
			continue
		}
		attached[comment] = true
		if err := found.read(body); err != nil {
			return found, err
		}
	}
	if found.data != "" && found.inline != "" {
		return found, fmt.Errorf("%w: a declaration is both data %q and inline %q", errInvalidSource, found.data, found.inline)
	}
	return found, nil
}

// declarationDoc returns the doc comment of a top-level declaration.
//
// Takes declaration (ast.Decl) which is the declaration.
//
// Returns *ast.CommentGroup which is its doc comment, or nil.
func declarationDoc(declaration ast.Decl) *ast.CommentGroup {
	switch typed := declaration.(type) {
	case *ast.GenDecl:
		return typed.Doc
	case *ast.FuncDecl:
		return typed.Doc
	default:
		return nil
	}
}

// parseSpecDirectives reads the directives of the doc comment of every spec of a grouped
// const, var or type declaration.
//
// Takes declaration (ast.Decl) which is the declaration.
// Takes attached (map[*ast.Comment]bool) which records each directive comment read.
//
// Returns []directives which holds the directives of each spec, or nil when the
// declaration is not grouped or none of its specs has a directive.
// Returns error when a directive is unknown or malformed, or is on a spec of a const
// block whose values follow from the specs before, which removing a spec would change.
func parseSpecDirectives(declaration ast.Decl, attached map[*ast.Comment]bool) ([]directives, error) {
	general, isGeneral := declaration.(*ast.GenDecl)
	if !isGeneral || !general.Lparen.IsValid() {
		return nil, nil
	}
	found := make([]directives, len(general.Specs))
	directed := false
	for index, spec := range general.Specs {
		read, err := parseDirectives(specDoc(spec), attached)
		if err != nil {
			return nil, err
		}
		found[index] = read
		directed = directed || read.data != "" || read.inline != "" || len(read.when) > 0
	}
	if !directed {
		return nil, nil
	}
	if general.Tok == token.CONST && repeatsValues(general) {
		return nil, fmt.Errorf("%w: a spec of a const block that counts with iota or repeats its values has a directive",
			errInvalidSource)
	}
	return found, nil
}

// repeatsValues reports whether a const block has a spec that leaves out its values,
// repeating those before it, or counts with iota.
//
// Takes general (*ast.GenDecl) which is the const block.
//
// Returns bool which is true when a spec's value depends on its place in the block.
func repeatsValues(general *ast.GenDecl) bool {
	for _, spec := range general.Specs {
		value, isValue := spec.(*ast.ValueSpec)
		if !isValue || len(value.Values) == 0 {
			return true
		}
		for _, expression := range value.Values {
			for node := range ast.Preorder(expression) {
				if identifier, isIdentifier := node.(*ast.Ident); isIdentifier && identifier.Name == "iota" {
					return true
				}
			}
		}
	}
	return false
}

// singleSpec returns the one spec a const, var or type declaration holds.
//
// Takes declaration (ast.Decl) which is the declaration.
//
// Returns ast.Spec which is the spec, or nil unless the declaration holds exactly one.
func singleSpec(declaration ast.Decl) ast.Spec {
	general, isGeneral := declaration.(*ast.GenDecl)
	if !isGeneral || len(general.Specs) != 1 {
		return nil
	}
	return general.Specs[0]
}

// specDoc returns the doc comment of a const, var or type spec.
//
// Takes spec (ast.Spec) which is the spec.
//
// Returns *ast.CommentGroup which is its doc comment, or nil.
func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch typed := spec.(type) {
	case *ast.ValueSpec:
		return typed.Doc
	case *ast.TypeSpec:
		return typed.Doc
	default:
		return nil
	}
}

// specStart returns where a spec starts, counting its doc comment.
//
// Takes spec (ast.Spec) which is the spec.
//
// Returns token.Pos which is the start of its doc comment, or of the spec without one.
func specStart(spec ast.Spec) token.Pos {
	if doc := specDoc(spec); doc != nil {
		return doc.Pos()
	}
	return spec.Pos()
}

// specName returns the one name a const, var or type spec declares.
//
// Takes spec (ast.Spec) which is the spec, or nil.
//
// Returns string which is the name.
// Returns bool which is false unless the spec declares exactly one name.
func specName(spec ast.Spec) (string, bool) {
	switch typed := spec.(type) {
	case *ast.ValueSpec:
		if len(typed.Names) != 1 {
			return "", false
		}
		return typed.Names[0].Name, true
	case *ast.TypeSpec:
		return typed.Name.Name, true
	default:
		return "", false
	}
}

// dataTarget returns the expression a data spec's value replaces.
//
// Takes spec (ast.Spec) which is the spec, or nil.
//
// Returns ast.Expr which is the declared value, or its sole argument when it is a call.
// Returns bool which is false unless the spec is a const or var with one value.
func dataTarget(spec ast.Spec) (ast.Expr, bool) {
	value, isValue := spec.(*ast.ValueSpec)
	if !isValue || len(value.Values) != 1 {
		return nil, false
	}
	if call, isCall := value.Values[0].(*ast.CallExpr); isCall && len(call.Args) == 1 {
		return call.Args[0], true
	}
	return value.Values[0], true
}

// isDirective reports whether a comment is a directive or nolint comment.
//
// Takes text (string) which is the comment's text.
//
// Returns bool which is true for a directive.
func isDirective(text string) bool {
	return strings.HasPrefix(text, directivePrefix) || strings.HasPrefix(text, nolintPrefix)
}

// documentation returns the comment groups a node carries as its documentation: the doc
// comment of a declaration, and the doc and line comments of a spec or a field.
//
// Takes node (ast.Node) which is any node of the source.
//
// Returns []*ast.CommentGroup which holds the groups, some of which may be nil.
func documentation(node ast.Node) []*ast.CommentGroup {
	switch typed := node.(type) {
	case *ast.GenDecl:
		return []*ast.CommentGroup{typed.Doc}
	case *ast.FuncDecl:
		return []*ast.CommentGroup{typed.Doc}
	case *ast.TypeSpec:
		return []*ast.CommentGroup{typed.Doc, typed.Comment}
	case *ast.ValueSpec:
		return []*ast.CommentGroup{typed.Doc, typed.Comment}
	case *ast.Field:
		return []*ast.CommentGroup{typed.Doc, typed.Comment}
	default:
		return nil
	}
}
