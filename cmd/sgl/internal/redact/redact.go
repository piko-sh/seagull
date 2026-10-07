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

// Package redact hides passwords and other secrets in database connection strings, and in
// any text that quotes one, before sgl prints them.
package redact

import (
	"cmp"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const (
	// Mask replaces every secret in a printed connection string.
	Mask = "xxxxx"

	// passwordSeparator separates the user name from the password in user information.
	passwordSeparator = ":"
)

var (
	// urlPrefix matches the scheme and "://" that open a URL connection string.
	urlPrefix = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
	// assignment matches one key=value pair in free text, with an optionally quoted value.
	// An unterminated quote runs to the end of the text, so its value is still hidden.
	assignment = regexp.MustCompile(`([A-Za-z0-9_.-]+)(\s*=\s*)('(?:[^'\\]|\\.)*'?|"(?:[^"\\]|\\.)*"?|[^\s;&]*)`)

	// keyPattern is the shape of a key in a libpq key=value connection string.
	keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// secretKeyFragments lists the parts of a parameter name that mark its value as secret.
	secretKeyFragments = []string{"password", "passwd", "secret", "token"}

	// secretKeySuffixes lists the endings of a parameter name that mark its value as secret,
	// such as _auth_pass.
	secretKeySuffixes = []string{"_pass", "_pwd"}

	// secretKeyNames lists short parameter names whose value is secret.
	secretKeyNames = []string{"pwd", "pass"}
)

// Writer redacts registered connection strings from everything written through it, so a
// driver error or log record that quotes one never reaches the terminal intact.
//
// Concurrency: safe for use by several goroutines; each Write is redacted and passed on
// whole under a lock.
type Writer struct {
	// target receives the redacted output.
	target io.Writer

	// connectionStrings lists the registered connection strings.
	connectionStrings []string

	// mutex guards connectionStrings and serialises writes.
	mutex sync.Mutex
}

// NewWriter wraps a writer so registered connection strings are redacted.
//
// Takes target (io.Writer) which receives the redacted output.
//
// Returns *Writer which passes output through unchanged until a connection string is
// added.
func NewWriter(target io.Writer) *Writer {
	return &Writer{target: target, connectionStrings: nil, mutex: sync.Mutex{}}
}

// Add registers a connection string to redact from later writes. Empty and repeated
// strings are ignored.
//
// Takes dsn (string) which is the connection string.
//
// Concurrency: the writer mutex serialises registration and redacted writes.
func (w *Writer) Add(dsn string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if dsn == "" || slices.Contains(w.connectionStrings, dsn) {
		return
	}
	w.connectionStrings = append(w.connectionStrings, dsn)
}

// Write redacts every registered connection string and its secrets from data, then writes
// the result to the target.
//
// Takes data ([]byte) which is the output.
//
// Returns int which is len(data) when the redacted text was written in full.
// Returns error when the target fails.
//
// Concurrency: the writer mutex serialises registration and output.
func (w *Writer) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if len(w.connectionStrings) == 0 {
		return w.target.Write(data)
	}
	text := string(data)
	for _, dsn := range w.connectionStrings {
		text = Text(text, dsn)
	}
	if _, err := io.WriteString(w.target, text); err != nil {
		return 0, err
	}
	return len(data), nil
}

// keyValue is one pair of a libpq key=value connection string.
type keyValue struct {
	// key is the parameter name.
	key string

	// value is the value with its quotes removed and its escapes resolved.
	value string

	// raw is the value as written, without its surrounding quotes.
	raw string

	// start is the byte offset where the value, including any opening quote, begins.
	start int

	// end is the byte offset just after the value, including any closing quote.
	end int
}

// result holds a redacted connection string and the secrets taken out of it.
type result struct {
	// text is the connection string with every secret replaced by Mask.
	text string

	// secrets lists every secret removed, as written and decoded.
	secrets []string
}

// DSN hides every secret in a connection string so it can be printed.
//
// It understands URLs (passwords in the user information and password-like query
// parameters), MySQL "user:password@protocol(address)/database" strings, libpq key=value
// strings with quoted values, escapes and spaces around "=", and other key=value lists.
// When a URL cannot be parsed, everything from "://" to the last "@" is hidden, or the
// whole value when there is no "@".
//
// Takes dsn (string) which is the connection string.
//
// Returns string which is safe to print.
func DSN(dsn string) string {
	return scan(dsn).text
}

// Text hides a connection string and each secret inside it wherever they appear in text,
// such as a driver's error message.
//
// Takes text (string) which is the message to clean.
// Takes dsn (string) which is the connection string the message may quote.
//
// Returns string which is safe to print.
func Text(text, dsn string) string {
	if dsn == "" {
		return text
	}
	scanned := scan(dsn)
	parts := strings.Split(text, dsn)
	for index := range parts {
		for _, secret := range scanned.secrets {
			parts[index] = strings.ReplaceAll(parts[index], secret, Mask)
		}
	}
	return strings.Join(parts, scanned.text)
}

// scan redacts a connection string and collects the secrets it held.
//
// Takes dsn (string) which is the connection string.
//
// Returns result which holds the redacted text and the secrets, longest first.
func scan(dsn string) result {
	var scanned result
	switch {
	case dsn == "":
		return scanned
	case urlPrefix.MatchString(dsn):
		scanned = scanURL(dsn)
	default:
		scanned = scanPlain(dsn)
	}
	scanned = scanAssignments(scanned)
	scanned.secrets = uniqueSecrets(scanned.secrets)
	return scanned
}

// scanPlain redacts a connection string that is not a URL: a libpq key=value string or a
// MySQL user:password@ string.
//
// Takes dsn (string) which is the connection string.
//
// Returns result which holds the redacted text and the secrets.
func scanPlain(dsn string) result {
	if pairs, ok := parseKeyValues(dsn); ok {
		return redactKeyValues(dsn, pairs)
	}
	return scanUserInformation(dsn)
}

// scanURL redacts a URL connection string.
//
// The structured path is taken only when the URL parses and the "@" that ends its user
// information is the last one, so a password holding "#", "?", "/" or "@" that misleads
// the parser falls through to hiding everything between "://" and the last "@".
//
// Takes dsn (string) which starts with a scheme and "://".
//
// Returns result which holds the redacted text and the secrets.
func scanURL(dsn string) result {
	prefixLength := len(urlPrefix.FindString(dsn))
	prefix, rest := dsn[:prefixLength], dsn[prefixLength:]
	lastAt := strings.LastIndexByte(rest, '@')
	if _, err := url.Parse(dsn); err == nil && userInformationEnd(rest) == lastAt {
		return scanParsedURL(prefix, rest, lastAt)
	}
	if lastAt < 0 {
		return result{text: prefix + Mask, secrets: nil}
	}
	userInformation := rest[:lastAt]
	user, password, hasPassword := strings.Cut(userInformation, passwordSeparator)
	if !hasPassword {
		return result{text: prefix + Mask + rest[lastAt:], secrets: decodedForms(userInformation)}
	}
	return result{text: prefix + user + passwordSeparator + Mask + rest[lastAt:], secrets: decodedForms(password)}
}

// userInformationEnd mirrors how net/url splits a URL to find the "@" that ends the user
// information.
//
// Takes rest (string) which is the URL after "://".
//
// Returns int which is the offset of that "@" in rest, or -1 when there is none.
func userInformationEnd(rest string) int {
	authority, _, _ := strings.Cut(rest, "#")
	authority, _, _ = strings.Cut(authority, "?")
	authority, _, _ = strings.Cut(authority, "/")
	return strings.LastIndexByte(authority, '@')
}

// scanParsedURL redacts a URL that net/url parses as written: the password in the user
// information and every secret query parameter.
//
// Takes prefix (string) which is the scheme and "://".
// Takes rest (string) which is the URL after the prefix.
// Takes at (int) which is the offset of the "@" ending the user information, or -1.
//
// Returns result which holds the redacted text and the secrets.
func scanParsedURL(prefix, rest string, at int) result {
	var secrets []string
	if at >= 0 {
		if user, password, hasPassword := strings.Cut(rest[:at], passwordSeparator); hasPassword {
			secrets = append(secrets, decodedForms(password)...)
			rest = user + passwordSeparator + Mask + rest[at:]
		}
	}
	beforeFragment, fragment, hasFragment := strings.Cut(rest, "#")
	beforeQuery, rawQuery, hasQuery := strings.Cut(beforeFragment, "?")
	if !hasQuery {
		return result{text: prefix + rest, secrets: secrets}
	}
	query, querySecrets := redactQuery(rawQuery)
	secrets = append(secrets, querySecrets...)
	text := prefix + beforeQuery + "?" + query
	if hasFragment {
		text += "#" + fragment
	}
	return result{text: text, secrets: secrets}
}

// redactQuery hides the value of every secret parameter in a raw URL query, leaving the
// order and spelling of the others alone.
//
// Takes query (string) which is the raw query without its "?".
//
// Returns string which is the redacted query.
// Returns []string which lists the hidden values.
func redactQuery(query string) (string, []string) {
	parameters := strings.Split(query, "&")
	var secrets []string
	for index, parameter := range parameters {
		key, value, hasValue := strings.Cut(parameter, "=")
		decodedKey, err := url.QueryUnescape(key)
		if err != nil {
			decodedKey = key
		}
		if !hasValue || !isSecretKey(decodedKey) {
			continue
		}
		secrets = append(secrets, decodedForms(value)...)
		parameters[index] = key + "=" + Mask
	}
	return strings.Join(parameters, "&"), secrets
}

// scanUserInformation redacts a MySQL "user:password@protocol(address)/database" string.
// Like go-sql-driver/mysql, the user information ends at the last "@" before the last
// "/", so a password may hold "@" or "/".
//
// Takes dsn (string) which is the connection string.
//
// Returns result which holds the redacted text and the secrets.
func scanUserInformation(dsn string) result {
	address := dsn
	if beforeSlash, _, hasSlash := strings.CutLast(dsn, "/"); hasSlash && strings.Contains(beforeSlash, "@") {
		address = beforeSlash
	}
	userInformation, _, hasAt := strings.CutLast(address, "@")
	if !hasAt || userInformation == "" {
		return result{text: dsn, secrets: nil}
	}
	user, password, hasPassword := strings.Cut(userInformation, passwordSeparator)
	if !hasPassword {
		return result{text: dsn, secrets: nil}
	}
	return result{text: user + passwordSeparator + Mask + dsn[len(userInformation):], secrets: decodedForms(password)}
}

// scanAssignments hides the value of every secret key=value pair left in the text, which
// covers semicolon-separated lists, query strings outside URLs and libpq strings that
// could not be tokenised, such as one with an unterminated quote.
//
// Takes scanned (result) which holds the text so far.
//
// Returns result which holds the further redacted text and all the secrets.
func scanAssignments(scanned result) result {
	scanned.text = assignment.ReplaceAllStringFunc(scanned.text, func(match string) string {
		parts := assignment.FindStringSubmatch(match)
		key, separator, value := parts[1], parts[2], parts[3]
		if !isSecretKey(key) || value == "" || value == Mask {
			return match
		}
		scanned.secrets = append(scanned.secrets, unquoted(value)...)
		return key + separator + Mask
	})
	return scanned
}

// parseKeyValues tokenises a libpq key=value connection string: keys are identifiers, "="
// may have spaces around it, and a value is either single-quoted with backslash escapes
// or runs to the next space with backslash escapes.
//
// Takes text (string) which is the connection string.
//
// Returns []keyValue which holds every pair in order.
// Returns bool which is false when the text is not a well-formed key=value string.
func parseKeyValues(text string) ([]keyValue, bool) {
	var pairs []keyValue
	position := skipSpaces(text, 0)
	for position < len(text) {
		keyStart := position
		for position < len(text) && text[position] != '=' && !isSpace(text[position]) {
			position++
		}
		key := text[keyStart:position]
		position = skipSpaces(text, position)
		if !keyPattern.MatchString(key) || position >= len(text) || text[position] != '=' {
			return nil, false
		}
		position = skipSpaces(text, position+1)
		pair, ok := readValue(text, position)
		if !ok {
			return nil, false
		}
		pair.key = key
		pairs = append(pairs, pair)
		position = skipSpaces(text, pair.end)
	}
	return pairs, len(pairs) > 0
}

// readValue reads one libpq value starting at position.
//
// Takes text (string) which is the connection string.
// Takes position (int) which is where the value starts.
//
// Returns keyValue which holds the value, its raw spelling and its extent.
// Returns bool which is false for an unterminated quote or a trailing backslash.
func readValue(text string, position int) (keyValue, bool) {
	pair := keyValue{key: "", value: "", raw: "", start: position, end: position}
	quoted := position < len(text) && text[position] == '\''
	if quoted {
		position++
	}
	rawStart := position
	var value strings.Builder
	for position < len(text) {
		character := text[position]
		switch {
		case character == '\\':
			if position+1 >= len(text) {
				return pair, false
			}
			value.WriteByte(text[position+1])
			position += 2
			continue
		case quoted && character == '\'':
			pair.value, pair.raw, pair.end = value.String(), text[rawStart:position], position+1
			return pair, true
		case !quoted && isSpace(character):
			pair.value, pair.raw, pair.end = value.String(), text[rawStart:position], position
			return pair, true
		}
		value.WriteByte(character)
		position++
	}
	if quoted {
		return pair, false
	}
	pair.value, pair.raw, pair.end = value.String(), text[rawStart:position], position
	return pair, true
}

// redactKeyValues replaces the value of every secret pair with Mask.
//
// Takes text (string) which is the connection string.
// Takes pairs ([]keyValue) which are its tokens.
//
// Returns result which holds the redacted text and the secrets.
func redactKeyValues(text string, pairs []keyValue) result {
	var builder strings.Builder
	var secrets []string
	last := 0
	for _, pair := range pairs {
		if !isSecretKey(pair.key) {
			continue
		}
		builder.WriteString(text[last:pair.start])
		builder.WriteString(Mask)
		last = pair.end
		secrets = append(secrets, pair.value, pair.raw)
	}
	builder.WriteString(text[last:])
	return result{text: builder.String(), secrets: secrets}
}

// isSecretKey reports whether a parameter name holds a secret.
//
// Takes key (string) which is the parameter name.
//
// Returns bool which is true for password-like names in any case.
func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	if slices.Contains(secretKeyNames, lower) {
		return true
	}
	for _, suffix := range secretKeySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	for _, fragment := range secretKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// unquoted returns a free-text value with and without its surrounding quotes.
//
// Takes value (string) which is the value as written.
//
// Returns []string which holds both spellings.
func unquoted(value string) []string {
	trimmed := strings.Trim(value, `'"`)
	return []string{value, trimmed}
}

// decodedForms returns a value as written and percent-decoded, since a driver may quote
// either.
//
// Takes value (string) which is the value as written.
//
// Returns []string which holds the spellings.
func decodedForms(value string) []string {
	forms := []string{value}
	if decoded, err := url.PathUnescape(value); err == nil {
		forms = append(forms, decoded)
	}
	return forms
}

// uniqueSecrets drops empty and repeated secrets and orders the rest longest first, so a
// secret is replaced before any shorter one inside it.
//
// Takes secrets ([]string) which are the collected secrets.
//
// Returns []string which holds each secret once.
func uniqueSecrets(secrets []string) []string {
	secrets = slices.DeleteFunc(secrets, func(secret string) bool { return secret == "" || secret == Mask })
	slices.SortFunc(secrets, func(first, second string) int {
		return cmp.Or(cmp.Compare(len(second), len(first)), strings.Compare(first, second))
	})
	return slices.Compact(secrets)
}

// skipSpaces moves past ASCII white space.
//
// Takes text (string) which is scanned.
// Takes position (int) which is where to start.
//
// Returns int which is the first offset that is not white space.
func skipSpaces(text string, position int) int {
	for position < len(text) && isSpace(text[position]) {
		position++
	}
	return position
}

// isSpace reports whether a byte is ASCII white space.
//
// Takes character (byte) which is tested.
//
// Returns bool which is true for space, tab, newline, carriage return, form feed and
// vertical tab.
func isSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
