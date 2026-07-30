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

/*
#cgo pkg-config: varnishapi
#include <stdio.h>
#include <vapi/vsl.h>

void
set_bits(int tag, void *priv)
{
	unsigned char *bits = (unsigned char *)priv;
	unsigned char b = (unsigned char)tag;
	bits[b/8] |= 1 << (b & 7);
}

int
globWrap(_GoString_ goglob, unsigned char *bitmap)
{
	const char *glob = _GoStringPtr(goglob);
	size_t len = _GoStringLen(goglob);
	return VSL_Glob2Tags(glob, len, set_bits, (void *)bitmap);
}
*/
import "C"

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type qLHS struct {
	tags   [32]byte
	prefix []byte
	field  int
	level  int
	lvlCmp int
	vxid   bool
}

type rhsType uint8

const (
	empty rhsType = iota
	integer
	float
	strType
	regex
)

type qRHS struct {
	strVal   []byte
	intVal   int64
	floatVal float64
	regex    *regexp.Regexp
	rhsType  rhsType
}

type qExpr struct {
	a   *qExpr
	b   *qExpr
	lhs qLHS
	rhs qRHS
	tok qTokenType
}

type qParser struct {
	lexer qLexer
	tok   qToken
}

func (parser *qParser) err(msg string) QueryParseErr {
	pos := parser.lexer.scanner.Pos()
	return QueryParseErr{Msg: msg, Line: pos.Line, Column: pos.Column}
}

func (parser *qParser) unexpected(exp string) QueryParseErr {
	msg := fmt.Sprintf("Expected %s got '%s'", exp, parser.tok.val)
	return parser.err(msg)
}

func (parser *qParser) advToken() error {
	tok, err := parser.lexer.nextToken()
	if err != nil {
		return err
	}
	parser.tok = tok
	return nil
}

func (parser *qParser) parseLHS() (qLHS, error) {
	lhs := qLHS{level: -1}
	if parser.tok.tokType == vxid {
		lhs.vxid = true
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		return lhs, nil
	}
	if parser.tok.tokType == char && parser.tok.scanType == '{' {
		// level
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != val {
			return lhs, parser.unexpected("integer")
		}
		lvlStr := parser.tok.val
		len := len(lvlStr)
		last := lvlStr[len-1 : len]
		if last == "+" || last == "-" {
			lvlStr = lvlStr[:len-1]
			if last == "+" {
				lhs.lvlCmp = 1
			} else {
				lhs.lvlCmp = -1
			}
		}
		lvl, err := strconv.Atoi(lvlStr)
		if err != nil {
			return lhs, parser.err("Syntax error in level limit")
		}
		if lvl < 0 {
			return lhs, parser.err("Expected non-negative level")
		}
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != char || parser.tok.scanType != '}' {
			return lhs, parser.unexpected("'}'")
		}
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
	}
	for {
		if parser.tok.tokType != val {
			return lhs, parser.unexpected("VSL tag name")
		}
		i := C.globWrap(parser.tok.val, (*C.uchar)(&lhs.tags[0]))
		switch i {
		case -1:
			return lhs, parser.err("Tag name matches zero tags")
		case -2:
			return lhs, parser.err("Tag name is ambiguous")
		case -3:
			return lhs, parser.err("Syntax error in tag name")
		}
		if i < 0 {
			panic("Unexpected return from VSL_Glob2Tags")
		}
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != char || parser.tok.scanType != ',' {
			break
		}
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
	}
	if parser.tok.tokType == char && parser.tok.scanType == ':' {
		// Record prefix
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != val {
			return lhs, parser.unexpected("string")
		}
		lhs.prefix = []byte(parser.tok.val)
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
	}
	if parser.tok.tokType == char && parser.tok.scanType == '[' {
		// LHS field []
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != val {
			return lhs, parser.unexpected("integer")
		}
		fld, err := strconv.Atoi(parser.tok.val)
		if err != nil {
			return lhs, parser.err("Syntax error in record field")
		}
		if fld <= 0 {
			return lhs, parser.err("Expected positive integer")
		}
		lhs.field = fld
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
		if parser.tok.tokType != char || parser.tok.scanType != ']' {
			return lhs, parser.unexpected("']'")
		}
		if err := parser.advToken(); err != nil {
			return lhs, err
		}
	}
	return lhs, nil
}

func (parser *qParser) parseNum(intOnly bool) (qRHS, error) {
	rhs := qRHS{}
	if parser.tok.tokType != val {
		return rhs, parser.unexpected("number")
	}
	num := parser.tok.val
	if strings.Contains(num, ".") {
		if intOnly {
			return rhs, parser.unexpected("integer")
		}
		flt, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return rhs, parser.err("Floating point parse error")
		}
		rhs.rhsType = float
		rhs.floatVal = flt
		if err := parser.advToken(); err != nil {
			return rhs, err
		}
		return rhs, nil
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return rhs, parser.err("Integer parse error")
	}
	rhs.rhsType = integer
	rhs.intVal = int64(n)
	if err := parser.advToken(); err != nil {
		return rhs, err
	}
	return rhs, nil
}

func (parser *qParser) parseStr() (qRHS, error) {
	rhs := qRHS{rhsType: strType}
	if parser.tok.tokType != val {
		return rhs, parser.unexpected("string")
	}
	rhs.strVal = []byte(parser.tok.val)
	if err := parser.advToken(); err != nil {
		return rhs, err
	}
	return rhs, nil
}

func (parser *qParser) parseRegex() (qRHS, error) {
	rhs := qRHS{rhsType: regex}
	if parser.tok.tokType != val {
		return rhs, parser.unexpected("regular expression")
	}
	regex, err := regexp.Compile(parser.tok.val)
	if err != nil {
		msg := "Regular expression error: " + err.Error()
		return rhs, parser.err(msg)
	}
	rhs.regex = regex
	rhs.strVal = []byte(parser.tok.val)
	if err := parser.advToken(); err != nil {
		return rhs, err
	}
	return rhs, nil
}

func (parser *qParser) parseExprCmp() (*qExpr, error) {
	lhs, err := parser.parseLHS()
	if err != nil {
		return nil, err
	}
	expr := new(qExpr)
	expr.lhs = lhs
	t := parser.tok.tokType
	if lhs.vxid {
		if t <= numericBegin || t >= numericEnd {
			return nil, parser.unexpected("numeric operator")
		}
	}
	switch t {
	case eoi, and, or, char:
		if t == char && parser.tok.scanType != ')' {
			break
		}
		expr.rhs.rhsType = empty
		return expr, nil
	}
	if t <= numericBegin || t >= stringEnd {
		return nil, parser.unexpected("operator")
	}
	expr.tok = t
	if err := parser.advToken(); err != nil {
		return nil, err
	}
	var rhs qRHS
	switch {
	case t > numericBegin && t < numericEnd:
		rhs, err = parser.parseNum(lhs.vxid)
	case t > stringBegin && t < stringEnd:
		rhs, err = parser.parseStr()
	case t > regexBegin && t < regexEnd:
		rhs, err = parser.parseRegex()
	default:
		panic("comparison operator out of range")
	}
	if err != nil {
		return nil, err
	}
	expr.rhs = rhs
	return expr, nil
}

func (parser *qParser) parseExprGrp() (*qExpr, error) {
	if parser.tok.tokType == char && parser.tok.scanType == '(' {
		if err := parser.advToken(); err != nil {
			return nil, err
		}
		expr, err := parser.parseOr()
		if err != nil {
			return nil, err
		}
		if parser.tok.tokType != char || parser.tok.scanType != ')' {
			return nil, parser.unexpected("')'")
		}
		if err := parser.advToken(); err != nil {
			return nil, err
		}
		return expr, nil
	}
	expr, err := parser.parseExprCmp()
	if err != nil {
		return nil, err
	}
	return expr, nil
}

func (parser *qParser) parseNot() (*qExpr, error) {
	if parser.tok.tokType == not {
		expr := new(qExpr)
		expr.tok = not
		if err := parser.advToken(); err != nil {
			return nil, err
		}
		exprGrp, err := parser.parseExprGrp()
		if err != nil {
			return nil, err
		}
		expr.a = exprGrp
		return expr, nil
	}
	expr, err := parser.parseExprGrp()
	if err != nil {
		return nil, err
	}
	return expr, nil
}

func (parser *qParser) parseAnd() (*qExpr, error) {
	expr, err := parser.parseNot()
	if err != nil {
		return nil, err
	}
	for parser.tok.tokType == and {
		a := expr
		expr = new(qExpr)
		expr.a = a
		expr.tok = and
		if err := parser.advToken(); err != nil {
			return nil, err
		}
		notExpr, err := parser.parseNot()
		if err != nil {
			return nil, err
		}
		expr.b = notExpr
	}
	return expr, nil
}

func (parser *qParser) parseOr() (*qExpr, error) {
	expr, err := parser.parseAnd()
	if err != nil {
		return nil, err
	}
	for parser.tok.tokType == or {
		a := expr
		expr = new(qExpr)
		expr.a = a
		expr.tok = or
		if err := parser.advToken(); err != nil {
			return nil, err
		}
		andExpr, err := parser.parseAnd()
		if err != nil {
			return nil, err
		}
		expr.b = andExpr
	}
	return expr, nil
}

func (parser *qParser) parseExpr() (*qExpr, error) {
	expr, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.tok.tokType != eoi {
		return nil, parser.unexpected("end of input")
	}
	return expr, nil
}

func parseQuery(query string) (*qExpr, error) {
	parser := &qParser{lexer: newLexer(query)}
	if err := parser.advToken(); err != nil {
		return nil, err
	}
	expr, err := parser.parseExpr()
	if err != nil {
		return nil, err
	}
	return expr, nil
}
