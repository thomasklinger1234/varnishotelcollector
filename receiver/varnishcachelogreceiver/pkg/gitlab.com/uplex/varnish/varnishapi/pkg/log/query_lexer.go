/*-
 * Copyright (c) 2018 UPLEX Nils Goroll Systemoptimierung
 * All rights reserved
 *
 * Author: Geoffrey Simmons <geoffrey.simmons@uplex.de>
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

package log

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/scanner"
)

type qTokenType rune

const (
	illegal qTokenType = iota
	eoi

	numericBegin
	eq
	neq
	lt
	leq
	gt
	geq
	numericEnd

	regexBegin
	match
	nomatch
	regexEnd

	stringBegin
	seq
	sneq
	stringEnd

	booleanBegin
	and
	or
	not
	booleanEnd

	val
	vxid
	char
)

var (
	operandChars = regexp.MustCompile("[-[:word:]+.*]")
)

type qToken struct {
	tokType  qTokenType
	scanType rune
	val      string
}

// QueryParseErr is the type of error returned by NewQuery() if the
// log query string cannot be parsed. Msg is the error message, Line
// is the line in which the error was found (>1 if the query contains
// newlines), and Column is the offset within the line at which the
// error was found.
type QueryParseErr struct {
	Msg    string
	Line   int
	Column int
}

// Error returns a formatted error message for a QueryParseErr (so
// that QueryParseErr implements the error interface).
func (qerr QueryParseErr) Error() string {
	return fmt.Sprintf("query parse error (line %d col %d): %s", qerr.Line,
		qerr.Column, qerr.Msg)
}

type qLexer struct {
	scanner *scanner.Scanner
	err     QueryParseErr
}

func operandRune(ch rune, i int) bool {
	b := []byte{byte(ch)}
	return operandChars.Match(b)
}

func newLexer(query string) qLexer {
	var s scanner.Scanner
	s.Mode ^= scanner.SkipComments
	s.Mode ^= scanner.ScanRawStrings
	s.Mode ^= scanner.ScanChars
	s.Mode ^= scanner.ScanInts
	s.Mode ^= scanner.ScanFloats
	s.IsIdentRune = operandRune

	rdr := strings.NewReader(query)
	s.Init(rdr)
	lexer := qLexer{scanner: &s}

	s.Error = func(s *scanner.Scanner, msg string) {
		pos := s.Pos()
		lexer.err = QueryParseErr{
			Msg:    msg,
			Line:   pos.Line,
			Column: pos.Column,
		}
	}
	return lexer
}

func (lexer qLexer) unexpected() error {
	s := lexer.scanner
	pos := s.Pos()
	msg := fmt.Sprintf("unexpected '%s'", s.TokenText())
	return QueryParseErr{
		Msg:    msg,
		Line:   pos.Line,
		Column: pos.Column,
	}
}

func (lexer qLexer) nextToken() (qToken, error) {
	s := lexer.scanner.Scan()
	tok := qToken{scanType: s}
	illTok := qToken{scanType: s, tokType: illegal, val: "illegal token"}
	if lexer.err.Msg != "" {
		return illTok, lexer.err
	}

	switch s {
	case scanner.EOF:
		tok.tokType = eoi
		tok.val = "end of input"
	case scanner.Ident:
		tok.val = lexer.scanner.TokenText()
		switch tok.val {
		case "eq":
			tok.tokType = seq
		case "ne":
			tok.tokType = sneq
		case "and":
			tok.tokType = and
		case "or":
			tok.tokType = or
		case "not":
			tok.tokType = not
		case "vxid":
			tok.tokType = vxid
		default:
			tok.tokType = val
		}
	case scanner.String:
		txt := lexer.scanner.TokenText()
		unquoted, err := strconv.Unquote(txt)
		if err != nil {
			pos := lexer.scanner.Pos()
			return illTok, QueryParseErr{
				Msg:    err.Error(),
				Line:   pos.Line,
				Column: pos.Column,
			}
		}
		tok.val = unquoted
		tok.tokType = val
	case '=':
		if lexer.scanner.Peek() != '=' {
			return illTok, lexer.unexpected()
		}
		lexer.scanner.Next()
		tok.tokType = eq
		tok.val = "=="
	case '<', '>':
		if lexer.scanner.Peek() == '=' {
			lexer.scanner.Next()
			if s == '<' {
				tok.tokType = leq
				tok.val = "<="
				break
			}
			tok.tokType = geq
			tok.val = ">="
			break
		}
		if s == '<' {
			tok.tokType = lt
			tok.val = "<"
			break
		}
		tok.tokType = gt
		tok.val = ">"
	case '!':
		switch lexer.scanner.Peek() {
		case '=':
			lexer.scanner.Next()
			tok.tokType = neq
			tok.val = "!="
		case '~':
			lexer.scanner.Next()
			tok.tokType = nomatch
			tok.val = "!~"
		default:
			return illTok, lexer.unexpected()
		}
	case '~':
		tok.tokType = match
		tok.val = "~"
	case '(', ')', ',', ':', '[', ']', '{', '}':
		tok.tokType = char
		tok.val = string(s)
	default:
		return illTok, lexer.unexpected()
	}
	return tok, nil
}
