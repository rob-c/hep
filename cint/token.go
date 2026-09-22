// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// kind is what sort of thing a token is.
type kind int

const (
	tokEOF kind = iota
	tokIdent
	tokInt
	tokFloat
	tokString
	tokChar
	tokPunct
)

func (k kind) String() string {
	switch k {
	case tokEOF:
		return "end of file"
	case tokIdent:
		return "name"
	case tokInt:
		return "integer"
	case tokFloat:
		return "number"
	case tokString:
		return "string"
	case tokChar:
		return "character"
	case tokPunct:
		return "operator"
	}
	return "token"
}

// token is one piece of a macro's source.
type token struct {
	kind kind
	text string
	line int
}

func (t token) String() string {
	if t.kind == tokEOF {
		return "end of file"
	}
	return fmt.Sprintf("%q", t.text)
}

// is reports whether the token is the given punctuation or keyword.
func (t token) is(text string) bool {
	return t.text == text && (t.kind == tokPunct || t.kind == tokIdent)
}

// operators, longest first so that "<<=" is not read as "<<" and "=".
var operators = []string{
	"<<=", ">>=", "...",
	"->", "::", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
	"+", "-", "*", "/", "%", "=", "<", ">", "!", "~", "&", "|", "^",
	"(", ")", "{", "}", "[", "]", ";", ",", ".", ":", "?",
}

// lexer turns a macro's source into tokens.
type lexer struct {
	src  string
	pos  int
	line int

	// includes are the headers the macro asked for, kept only so that the
	// translation can say what it ignored.
	includes []string
}

func newLexer(src string) *lexer {
	return &lexer{src: src, line: 1}
}

// lex reads every token, or says where it gave up.
func (l *lexer) lex() ([]token, error) {
	var out []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		if t.kind == tokEOF {
			return out, nil
		}
	}
}

func (l *lexer) errf(format string, args ...any) error {
	return fmt.Errorf("cint: line %d: %s", l.line, fmt.Sprintf(format, args...))
}

// skipSpace steps over whitespace, comments and preprocessor lines.
func (l *lexer) skipSpace() error {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++

		case c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f':
			l.pos++

		case c == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n':
			// a line continuation outside a string.
			l.line++
			l.pos += 2

		case strings.HasPrefix(l.src[l.pos:], "//"):
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}

		case strings.HasPrefix(l.src[l.pos:], "/*"):
			end := strings.Index(l.src[l.pos+2:], "*/")
			if end < 0 {
				return l.errf("a comment is opened and never closed")
			}
			l.line += strings.Count(l.src[l.pos:l.pos+2+end+2], "\n")
			l.pos += 2 + end + 2

		case c == '#':
			l.preproc()

		default:
			return nil
		}
	}
	return nil
}

// preproc steps over a preprocessor line.
//
// A macro's #includes say which ROOT headers it wanted, which a translation
// has no use for: what it maps onto is go-hep, whose packages are worked out
// from what the macro actually calls.
func (l *lexer) preproc() {
	beg := l.pos
	for l.pos < len(l.src) {
		if l.src[l.pos] == '\n' {
			// a directive continues onto the next line if this one ends
			// in a backslash.
			if l.pos > beg && l.src[l.pos-1] == '\\' {
				l.line++
				l.pos++
				continue
			}
			break
		}
		l.pos++
	}

	line := strings.TrimSpace(l.src[beg:l.pos])
	if rest, ok := strings.CutPrefix(line, "#include"); ok {
		name := strings.Trim(strings.TrimSpace(rest), `"<>`)
		if name != "" {
			l.includes = append(l.includes, name)
		}
	}
}

// next reads one token.
func (l *lexer) next() (token, error) {
	if err := l.skipSpace(); err != nil {
		return token{}, err
	}
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, line: l.line}, nil
	}

	var (
		line = l.line
		c    = l.src[l.pos]
	)

	switch {
	case isIdentStart(c):
		beg := l.pos
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		return token{kind: tokIdent, text: l.src[beg:l.pos], line: line}, nil

	case c >= '0' && c <= '9',
		c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]):
		return l.number(line)

	case c == '"':
		return l.quoted('"', tokString, line)

	case c == '\'':
		return l.quoted('\'', tokChar, line)
	}

	for _, op := range operators {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.pos += len(op)
			return token{kind: tokPunct, text: op, line: line}, nil
		}
	}

	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return token{}, l.errf("cannot read %q", r)
}

// number reads an integer or a floating-point literal, along with whatever
// suffix C++ lets it carry.
func (l *lexer) number(line int) (token, error) {
	beg := l.pos
	isFloat := false

	if strings.HasPrefix(l.src[l.pos:], "0x") || strings.HasPrefix(l.src[l.pos:], "0X") {
		l.pos += 2
		for l.pos < len(l.src) && isHex(l.src[l.pos]) {
			l.pos++
		}
	} else {
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
		if l.pos < len(l.src) && l.src[l.pos] == '.' {
			isFloat = true
			l.pos++
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
			isFloat = true
			l.pos++
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.pos++
			}
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
		}
	}

	text := l.src[beg:l.pos]

	// the suffixes: f, F, u, U, l, L and the combinations of them.
	for l.pos < len(l.src) && strings.ContainsRune("fFuUlL", rune(l.src[l.pos])) {
		if l.src[l.pos] == 'f' || l.src[l.pos] == 'F' {
			isFloat = true
		}
		l.pos++
	}

	k := tokInt
	if isFloat {
		k = tokFloat
	}
	return token{kind: k, text: text, line: line}, nil
}

// quoted reads a string or character literal, keeping the escapes as they
// were written: C++ and Go spell the ones that matter the same way.
func (l *lexer) quoted(q byte, k kind, line int) (token, error) {
	beg := l.pos
	l.pos++ // the opening quote
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case '\\':
			l.pos += 2
			continue
		case '\n':
			return token{}, l.errf("a %s is opened and never closed", k)
		case q:
			l.pos++
			return token{kind: k, text: l.src[beg:l.pos], line: line}, nil
		}
		l.pos++
	}
	return token{}, l.errf("a %s is opened and never closed", k)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c))
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
