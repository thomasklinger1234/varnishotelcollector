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
	"bytes"
	"testing"
)

type expLHS struct {
	tags   []string
	prefix string
	field  int
	level  int
	lvlCmp int
	vxid   bool
}

type expRHS struct {
	str      string
	intVal   int64
	floatVal float64
	rhsType  rhsType
}

type expExpr struct {
	pass bool
	aNil bool
	bNil bool
	lhs  expLHS
	rhs  expRHS
	tok  qTokenType
}

type expParse struct {
	query string
	expr  expExpr
}

func bitmap2Tags(bits [32]byte) []string {
	var strs []string
	for i, b := range bits {
		if b == 0 {
			continue
		}
		for j := range 8 {
			if (1<<uint(j))&b != 0 {
				tagnum := i*8 + j
				strs = append(strs, Tags[tagnum].String)
			}
		}
	}
	return strs
}

func checkLHS(t *testing.T, exp expLHS, got qLHS) {
	strs := bitmap2Tags(got.tags)
	for i, s := range exp.tags {
		if s != strs[i] {
			t.Errorf("lhs tag want=%v got=%v", s, strs[i])
		}
	}
	if !bytes.Equal(got.prefix, []byte(exp.prefix)) {
		t.Errorf("lhs prefix want=%v got=%v", exp.prefix,
			string(got.prefix))
	}
	if got.field != exp.field {
		t.Errorf("lhs field want=%v got=%v", exp.field, got.field)
	}
	if got.level != exp.level {
		t.Errorf("lhs level want=%v got=%v", exp.level, got.level)
	}
	if got.lvlCmp != exp.lvlCmp {
		t.Errorf("lhs level plus/minus want=%v got=%v", exp.lvlCmp,
			got.lvlCmp)
	}
}

func checkRHS(t *testing.T, exp expRHS, got qRHS) {
	if exp.rhsType != got.rhsType {
		t.Errorf("rhs type want=%v got=%v", exp.rhsType, got.rhsType)
		return
	}
	switch got.rhsType {
	case integer:
		if exp.intVal != got.intVal {
			t.Errorf("rhs intVal want=%v got=%v", exp.intVal,
				got.intVal)
		}
	case float:
		if exp.floatVal != got.floatVal {
			t.Errorf("rhs floatVal want=%v got=%v", exp.floatVal,
				got.floatVal)
		}
	case strType:
		if exp.str != string(got.strVal) {
			t.Errorf("rhs strVal want=%v got=%v", exp.str,
				string(got.strVal))
		}
	case regex:
		if got.regex == nil {
			t.Error("rhs regex want=true got=false")
		}
	}
}

// Examples from vsl-query(7), b00050.vtc, l00000.vtc, l00001.vtc
var expParses = []expParse{
	{query: `ReqURL eq "/foo"`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"ReqURL"},
				level: -1,
			},
			rhs: expRHS{
				str:     "/foo",
				rhsType: strType,
			},
			tok: seq,
		},
	},
	{query: `ReqHeader:cookie`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:   []string{"ReqHeader"},
				prefix: "cookie",
				level:  -1,
			},
			rhs: expRHS{rhsType: empty},
		},
	},
	{query: `not ReqHeader:cookie`,
		expr: expExpr{
			pass: true,
			aNil: false,
			bNil: true,
			tok:  not,
		},
	},
	{query: `Timestamp:Process[2] > 0.8`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:   []string{"Timestamp"},
				level:  -1,
				prefix: "Process",
				field:  2,
			},
			rhs: expRHS{
				floatVal: 0.8,
				rhsType:  float,
			},
			tok: gt,
		},
	},
	{query: `ReqHeader:user-agent ~ "iPod" and Timestamp:Resp[2] > 1.`,
		expr: expExpr{
			pass: true,
			aNil: false,
			bNil: false,
			tok:  and,
		},
	},
	{query: "BerespStatus >= 500",
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"BerespStatus"},
				level: -1,
			},
			rhs: expRHS{
				intVal:  500,
				rhsType: integer,
			},
			tok: geq,
		},
	},
	{query: "ReqStatus == 304 and not ReqHeader:if-modified-since",
		expr: expExpr{
			pass: true,
			aNil: false,
			bNil: false,
			tok:  and,
		},
	},
	{query: "BerespStatus >= 500 or {2+}Timestamp:Process[2] > 1.",
		expr: expExpr{
			pass: true,
			aNil: false,
			bNil: false,
			tok:  or,
		},
	},
	{query: "vxid == 0 and Error",
		expr: expExpr{
			pass: true,
			aNil: false,
			bNil: false,
			tok:  and,
		},
	},
	{query: "vxid == 1001",
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs:  expLHS{vxid: true},
			rhs: expRHS{
				intVal:  1001,
				rhsType: integer,
			},
			tok: eq,
		},
	},
	{query: "vxid ~ 1001"},
	{query: "vxid !~ 1001"},
	{query: "vxid eq 1001"},
	{query: "vxid ne 1001"},
	{query: "vxid != 1001.5"},
	{query: "vxid[1] >= 1001"},
	{query: "{1}vxid <= 1001"},
	{query: "vxid,Link > 1001"},
	{query: "vxid,vxid < 1001"},
	// XXX currently don't support single-quoted strings, text/scanner
	// does not interpret them as strings.
	// {query: "Begin ~ 'bereq 1001'",
	{query: `Begin ~ "bereq 1001"`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"Begin"},
				level: -1,
			},
			rhs: expRHS{
				str:     "bereq 1001",
				rhsType: regex,
			},
			tok: match,
		},
	},
	{query: `ReqProtocol ne "HTTP/1.0"`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"ReqProtocol"},
				level: -1,
			},
			rhs: expRHS{
				str:     "HTTP/1.0",
				rhsType: strType,
			},
			tok: sneq,
		},
	},
	{query: `RespStatus == 200`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"RespStatus"},
				level: -1,
			},
			rhs: expRHS{
				intVal:  200,
				rhsType: integer,
			},
			tok: eq,
		},
	},
	{query: `RespStatus == 200.`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"RespStatus"},
				level: -1,
			},
			rhs: expRHS{
				floatVal: 200.,
				rhsType:  float,
			},
			tok: eq,
		},
	},
	{query: `RespStatus != 503`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"RespStatus"},
				level: -1,
			},
			rhs: expRHS{
				intVal:  503,
				rhsType: integer,
			},
			tok: neq,
		},
	},
	{query: `RespStatus != 503.`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags:  []string{"RespStatus"},
				level: -1,
			},
			rhs: expRHS{
				floatVal: 503.,
				rhsType:  float,
			},
			tok: neq,
		},
	},
	{query: `Debug,Resp* == 200`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags: []string{"Debug", "RespMethod",
					"RespURL", "RespProtocol", "RespStatus",
					"RespReason", "RespHeader", "RespUnset",
					"RespLost"},
				level: -1,
			},
			rhs: expRHS{
				intVal:  200,
				rhsType: integer,
			},
			tok: eq,
		},
	},
	{query: `Resp*:x-test eq "123 321"`,
		expr: expExpr{
			pass: true,
			aNil: true,
			bNil: true,
			lhs: expLHS{
				tags: []string{"RespMethod", "RespURL",
					"RespProtocol", "RespStatus",
					"RespReason", "RespHeader", "RespUnset",
					"RespLost"},
				level:  -1,
				prefix: "x-test",
			},
			rhs: expRHS{
				str:     "123 321",
				rhsType: strType,
			},
			tok: seq,
		},
	},
}

func TestParser(t *testing.T) {
	for _, exp := range expParses {
		expr, err := parseQuery(exp.query)
		if exp.expr.pass && err != nil {
			t.Errorf("Could not parse '%s' as expected: %v",
				exp.query, err)
		}
		if !exp.expr.pass && err == nil {
			t.Errorf("Did not fail to parse '%s' as expected",
				exp.query)
		}
		if err != nil {
			continue
		}
		if exp.expr.aNil != (expr.a == nil) {
			t.Errorf("expr.a==nil want=%v got=%v", exp.expr.aNil,
				expr.a == nil)
		}
		if exp.expr.bNil != (expr.b == nil) {
			t.Errorf("expr.b==nil want=%v got=%v", exp.expr.bNil,
				expr.b == nil)
		}
		if exp.expr.tok != expr.tok {
			t.Errorf("expr.tok want=%v got=%v", exp.expr.tok,
				expr.tok)
		}
		if expr.a != nil {
			continue
		}
		if exp.expr.lhs.vxid != expr.lhs.vxid {
			t.Errorf("lhs vxid want=%v got=%v", exp.expr.lhs.vxid,
				expr.lhs.vxid)
		}
		if !expr.lhs.vxid {
			checkLHS(t, exp.expr.lhs, expr.lhs)
		}
		checkRHS(t, exp.expr.rhs, expr.rhs)
	}
}
