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

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// settingJoinUseNulls is the server setting that decides whether an outer join fills the
	// columns of an unmatched row with NULL or with each type's default value.
	settingJoinUseNulls = "join_use_nulls"

	// settingGroupByUseNulls is the server setting that decides whether a subtotal row of
	// ROLLUP, CUBE or GROUPING SETS fills a key it does not group by with NULL or with its
	// type's default value.
	settingGroupByUseNulls = "group_by_use_nulls"

	// settingAssignmentLength is the token count of a `name = value` setting assignment.
	settingAssignmentLength = 3
)

// sidedJoinModifiers holds the join kinds a LEFT or RIGHT join takes with each strictness
// keyword that may follow it.
type sidedJoinModifiers struct {
	// semi is the kind with SEMI.
	semi ast.JoinKind

	// anti is the kind with ANTI.
	anti ast.JoinKind

	// asof is the kind with ASOF, or the zero kind when the side has no ASOF form.
	asof ast.JoinKind

	// anyJoin is the kind with ANY.
	anyJoin ast.JoinKind

	// all is the kind with ALL.
	all ast.JoinKind
}

var (
	// joinGlobal is a GLOBAL join, a distributed-join broadcast hint: the right side is
	// computed once and sent to every shard, with inner-join row semantics.
	joinGlobal = ast.JoinKind{Keyword: "GLOBAL", Nullable: 0, Filters: true}

	// joinAny is an ANY join: for each left row, at most one matching right row, with
	// inner-join row semantics.
	joinAny = ast.JoinKind{Keyword: "ANY", Nullable: 0, Filters: true}

	// joinAll is an ALL join: for each left row, every matching right row, the default
	// strictness, with inner-join row semantics.
	joinAll = ast.JoinKind{Keyword: "ALL", Nullable: 0, Filters: true}

	// joinAsof is an ASOF join: each left row joins the closest matching right row, and a
	// left row without one is dropped.
	joinAsof = ast.JoinKind{Keyword: "ASOF", Nullable: 0, Filters: true}

	// joinSemi is a SEMI join: the left rows that have a matching right row.
	joinSemi = ast.JoinKind{Keyword: "SEMI", Nullable: 0, Filters: true}

	// joinAnti is an ANTI join: the left rows that have no matching right row.
	joinAnti = ast.JoinKind{Keyword: "ANTI", Nullable: 0, Filters: true}

	// joinLeftSemi is a LEFT SEMI join, the left-biased form of a SEMI join.
	joinLeftSemi = ast.JoinKind{Keyword: "LEFT SEMI", Nullable: 0, Filters: true}

	// joinLeftAnti is a LEFT ANTI join, the left-biased form of an ANTI join.
	joinLeftAnti = ast.JoinKind{Keyword: "LEFT ANTI", Nullable: 0, Filters: true}

	// joinRightSemi is a RIGHT SEMI join: the right rows that have a matching left row.
	joinRightSemi = ast.JoinKind{Keyword: "RIGHT SEMI", Nullable: 0, Filters: true}

	// joinRightAnti is a RIGHT ANTI join: the right rows that have no matching left row.
	joinRightAnti = ast.JoinKind{Keyword: "RIGHT ANTI", Nullable: 0, Filters: true}

	// joinLeftAsof is a LEFT ASOF join: every left row is kept, joined to the closest
	// matching right row when there is one.
	joinLeftAsof = ast.JoinKind{Keyword: "LEFT ASOF", Nullable: ast.JoinSideRight, Filters: false}

	// joinLeftAny is a LEFT ANY join: every left row is kept, joined to at most one matching
	// right row.
	joinLeftAny = ast.JoinKind{Keyword: "LEFT ANY", Nullable: ast.JoinSideRight, Filters: false}

	// joinLeftAll is a LEFT ALL join, the explicit form of a LEFT join.
	joinLeftAll = ast.JoinKind{Keyword: "LEFT ALL", Nullable: ast.JoinSideRight, Filters: false}

	// joinRightAny is a RIGHT ANY join: every right row is kept, joined to at most one
	// matching left row.
	joinRightAny = ast.JoinKind{Keyword: "RIGHT ANY", Nullable: ast.JoinSideLeft, Filters: false}

	// joinRightAll is a RIGHT ALL join, the explicit form of a RIGHT join.
	joinRightAll = ast.JoinKind{Keyword: "RIGHT ALL", Nullable: ast.JoinSideLeft, Filters: false}

	// leftJoinModifiers maps the strictness that may follow LEFT to its join kind.
	leftJoinModifiers = sidedJoinModifiers{semi: joinLeftSemi, anti: joinLeftAnti, asof: joinLeftAsof, anyJoin: joinLeftAny, all: joinLeftAll}

	// rightJoinModifiers maps the strictness that may follow RIGHT to its join kind; a RIGHT
	// ASOF join does not exist.
	rightJoinModifiers = sidedJoinModifiers{semi: joinRightSemi, anti: joinRightAnti, asof: ast.JoinKind{}, anyJoin: joinRightAny, all: joinRightAll}
)

// outerJoin determines an outer join's behaviour under join_use_nulls.
//
// When enabled, unmatched rows are nullable. Otherwise they use column defaults and
// retain their declared nullability.
//
// Takes kind (ast.JoinKind) which is the join's standard-SQL kind.
//
// Returns ast.JoinKind which is the kind with the sides that can really be NULL.
func (p *parser) outerJoin(kind ast.JoinKind) ast.JoinKind {
	if p.joinUseNulls {
		return kind
	}
	return ast.JoinKind{Keyword: kind.Keyword, Nullable: 0, Filters: kind.Filters}
}

// matchJoinKeyword recognises a JOIN-introducing keyword sequence and returns the
// corresponding JoinKind.
//
// ClickHouse's own joins are described as JoinKind values of this dialect, so the
// analyser reads their row semantics rather than their names. ASOF, SEMI, and ANTI are
// the classic ClickHouse join shapes, with LEFT and RIGHT-biased variants. The GLOBAL
// prefix is a distributed-join broadcast hint, recorded as joinGlobal when no other
// strictness modifier precedes it. The ANY and ALL strictness keywords are a ClickHouse
// multiplicity hint, recorded as joinAny or joinAll respectively when no body-specific
// keyword follows. When ANY or ALL is paired with a body keyword (INNER, LEFT, and
// similar) the body keyword wins; the strictness modifier is consumed transparently and
// the body kind is returned.
//
// Returns ast.JoinKind which is the matched join kind.
// Returns bool which is true when a join keyword sequence was consumed.
func (p *parser) matchJoinKeyword() (kind ast.JoinKind, matched bool) {
	saved := p.Position
	hasGlobal := p.MatchKeyword(kwGlobal)
	hasStrictness, strictnessKind := p.matchStrictnessPrefix()

	if (hasStrictness || hasGlobal) && p.IsKeyword(kwJoin) && !p.IsKeyword(kwInner, kwLeft, kwRight, kwFull, kwCross, kwAsof, kwSemi, kwAnti) {
		p.Advance()
		if hasStrictness {
			return strictnessKind, true
		}
		return joinGlobal, true
	}
	resolved, isResolved := p.matchJoinBody(saved)
	if isResolved {
		return resolved, true
	}
	if hasStrictness {
		if p.MatchKeyword(kwJoin) {
			return strictnessKind, true
		}
	}
	if hasGlobal {
		if p.MatchKeyword(kwJoin) {
			return joinGlobal, true
		}
	}
	p.Position = saved
	return ast.JoinKind{}, false
}

// matchStrictnessPrefix consumes an optional ClickHouse strictness keyword (ANY or ALL)
// that precedes the join body.
//
// Returns bool which is true when a strictness keyword was consumed, leaving the cursor
// unchanged otherwise.
// Returns ast.JoinKind which is the matched strictness join kind.
func (p *parser) matchStrictnessPrefix() (matched bool, kind ast.JoinKind) {
	switch {
	case p.MatchKeyword(kwAny):
		return true, joinAny
	case p.MatchKeyword(kwAll):
		return true, joinAll
	}
	return false, ast.JoinKind{}
}

// matchJoinBody attempts to consume the body of a join.
//
// The recognised bodies are INNER, LEFT, RIGHT, FULL, CROSS, ASOF, SEMI, ANTI, and plain
// JOIN. When no body keyword is consumed (and only strictness or GLOBAL prefixes have
// run), the caller falls back to its own handling.
//
// Takes saved (int) which is the cursor position to rewind to on a non-match.
//
// Returns ast.JoinKind which is the resolved join kind.
// Returns bool which is true when a body keyword sequence completed a JOIN match.
func (p *parser) matchJoinBody(saved int) (kind ast.JoinKind, resolved bool) {
	switch {
	case p.MatchKeyword(kwInner):
		p.MatchKeyword(kwJoin)
		return ast.JoinInner, true
	case p.MatchKeyword(kwLeft):
		return p.matchSidedJoin(saved, leftJoinModifiers, ast.JoinLeft)
	case p.MatchKeyword(kwRight):
		return p.matchSidedJoin(saved, rightJoinModifiers, ast.JoinRight)
	case p.MatchKeyword(kwFull):
		p.matchFullJoinStrictness()
		return p.matchOptionalOuterJoin(saved, p.outerJoin(ast.JoinFull))
	case p.MatchKeyword(kwCross):
		return p.matchPlainJoinKeyword(saved, ast.JoinCross)
	case p.MatchKeyword(kwAsof):
		p.MatchKeyword(kwInner)
		return p.matchPlainJoinKeyword(saved, joinAsof)
	case p.MatchKeyword(kwSemi):
		return p.matchPlainJoinKeyword(saved, joinSemi)
	case p.MatchKeyword(kwAnti):
		return p.matchPlainJoinKeyword(saved, joinAnti)
	case p.MatchKeyword(kwJoin):
		return ast.JoinInner, true
	}
	return ast.JoinKind{}, false
}

// matchSidedJoin handles the LEFT and RIGHT join variants, which share the same optional
// OUTER, optional SEMI, ANTI, ASOF, ANY or ALL strictness, then JOIN shape with
// side-specific JoinKind mappings. The kinds that keep every row of one side are outer
// joins, whose nullable side follows the join_use_nulls setting.
//
// Takes saved (int) which is the cursor position to rewind to on a non-match.
// Takes modifiers (sidedJoinModifiers) which maps each strictness to its kind.
// Takes plainKind (ast.JoinKind) which is the JoinKind for the plain JOIN form.
//
// Returns ast.JoinKind which is the matched join kind, or zero on a non-match.
// Returns bool which is true when a JOIN token followed, rewinding the cursor to saved
// otherwise.
func (p *parser) matchSidedJoin(saved int, modifiers sidedJoinModifiers, plainKind ast.JoinKind) (ast.JoinKind, bool) {
	p.MatchKeyword("OUTER")
	switch {
	case p.MatchKeyword(kwSemi):
		return p.matchPlainJoinKeyword(saved, modifiers.semi)
	case p.MatchKeyword(kwAnti):
		return p.matchPlainJoinKeyword(saved, modifiers.anti)
	case modifiers.asof.Keyword != "" && p.MatchKeyword(kwAsof):
		return p.matchPlainJoinKeyword(saved, p.outerJoin(modifiers.asof))
	case p.MatchKeyword(kwAny):
		return p.matchPlainJoinKeyword(saved, p.outerJoin(modifiers.anyJoin))
	case p.MatchKeyword(kwAll):
		return p.matchPlainJoinKeyword(saved, p.outerJoin(modifiers.all))
	}
	if p.MatchKeyword(kwJoin) {
		return p.outerJoin(plainKind), true
	}
	p.Position = saved
	return ast.JoinKind{}, false
}

// matchFullJoinStrictness consumes the ANY or ALL strictness that may follow FULL.
func (p *parser) matchFullJoinStrictness() {
	if !p.MatchKeyword(kwAny) {
		p.MatchKeyword(kwAll)
	}
}

// matchOptionalOuterJoin handles the FULL [OUTER] JOIN form.
//
// OUTER is consumed when present; JOIN is required, otherwise the cursor rewinds to
// saved.
//
// Takes saved (int) which is the cursor position to rewind to on a non-match.
// Takes joinKind (ast.JoinKind) which is the JoinKind to return on a match.
//
// Returns ast.JoinKind which is joinKind on a match, or zero on a non-match.
// Returns bool which is true when a JOIN token followed.
func (p *parser) matchOptionalOuterJoin(saved int, joinKind ast.JoinKind) (ast.JoinKind, bool) {
	p.MatchKeyword("OUTER")
	return p.matchPlainJoinKeyword(saved, joinKind)
}

// matchPlainJoinKeyword finalises a join match that has already consumed its preceding
// modifier such as SEMI, ANTI, or OUTER.
//
// JOIN must follow; otherwise the cursor is rewound to saved.
//
// Takes saved (int) which is the cursor position to rewind to on a non-match.
// Takes joinKind (ast.JoinKind) which is the JoinKind to return on a match.
//
// Returns ast.JoinKind which is joinKind on a match, or zero on a non-match.
// Returns bool which is true when a JOIN token followed.
func (p *parser) matchPlainJoinKeyword(saved int, joinKind ast.JoinKind) (ast.JoinKind, bool) {
	if p.MatchKeyword(kwJoin) {
		return joinKind, true
	}
	p.Position = saved
	return ast.JoinKind{}, false
}

// statementSetting reads a query's own `SETTINGS name = value` assignment of a boolean
// setting, such as join_use_nulls or group_by_use_nulls.
//
// Such a setting decides how the clauses before it, such as the query's outer joins or
// its GROUP BY, fill a row, so it must be known before they are walked; the statement's
// tokens are scanned for the assignment after a SETTINGS keyword. A value of 1 or true
// turns the setting on, and 0 or false turns it off.
//
// Takes tokens ([]token) which are the statement's tokens.
// Takes name (string) which is the setting's name.
//
// Returns bool which is the setting's value.
// Returns bool which is true when the statement sets it.
func statementSetting(tokens []token, name string) (enabled bool, found bool) {
	inSettings := false
	for index := range tokens {
		if tokens[index].IsKeyword(tokenIdentifier, kwSettings) {
			inSettings = true
			continue
		}
		if !inSettings || index+settingAssignmentLength > len(tokens) ||
			!tokens[index].IsKeyword(tokenIdentifier, name) ||
			tokens[index+1].Kind != tokenOperator || tokens[index+1].Value != "=" {
			continue
		}
		if value, valid := settingBooleanValue(tokens[index+2]); valid {
			enabled, found = value, true
		}
	}
	return enabled, found
}

// settingBooleanValue reads a boolean setting value: 1 or 0, true or false, or either
// form in a string literal.
//
// Takes candidate (token) which is the value token.
//
// Returns bool which is the value.
// Returns bool which is true when the token is a boolean setting value.
func settingBooleanValue(candidate token) (value bool, valid bool) {
	switch candidate.Kind {
	case tokenNumber, tokenString:
		switch candidate.Value {
		case "1":
			return true, true
		case "0":
			return false, true
		}
		return settingBooleanWord(candidate.Value)
	case tokenIdentifier:
		return settingBooleanWord(candidate.Value)
	default:
		return false, false
	}
}

// settingBooleanWord reads the words true and false, ignoring case.
//
// Takes word (string) which is the candidate word.
//
// Returns bool which is the value.
// Returns bool which is true when the word is true or false.
func settingBooleanWord(word string) (value bool, valid bool) {
	switch {
	case strings.EqualFold(word, "true"):
		return true, true
	case strings.EqualFold(word, "false"):
		return false, true
	default:
		return false, false
	}
}
