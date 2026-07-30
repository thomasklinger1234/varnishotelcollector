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

import "testing"

type queryTestCase struct {
	name  string
	query string
	pass  string
	fail  string
}

// Test vectors mostly adapted from l00001.vtc
var queryRecTestVec = []queryTestCase{
	{"seq", `Begin eq "req 118200423 rxreq"`, "req 118200423 rxreq",
		"req 118200424 rxreq"},
	{"sneq", `Begin ne "req 118200424 rxreq"`, "req 118200423 rxreq",
		"req 118200424 rxreq"},
	{"eqInt", `RespStatus == 200`, "200", "404"},
	{"eqFlt", "RespStatus == 200.", "200", "503"},
	{"neqInt", "RespStatus != 503", "200", "503"},
	{"neqFlt", "RespStatus != 503.", "200", "503"},
	{"ltInt", "RespStatus < 201", "200", "503"},
	{"ltFlt", "RespStatus < 201.", "200", "503"},
	{"gtInt", "RespStatus > 199", "200", "100"},
	{"gtFlt", "RespStatus > 199.", "200", "100"},
	{"leqInt", "RespStatus <= 200", "200", "503"},
	{"leqFlt", "RespStatus <= 200.", "200", "503"},
	{"geqInt", "RespStatus >= 200", "200", "100"},
	{"geqFlt", "RespStatus >= 200.", "200", "100"},
	{"match", `RespStatus ~ "^200$"`, "200", " 200 "},
	{"nomatch", `RespStatus !~ "^404$"`, "200", "404"},
	{"fldInt", "RespHeader[2] == 123", "x-test: 123 321",
		"x-test: 321 123"},
	{"fldFlt", "RespHeader[2] == 123.", "x-test: 123 321",
		"x-test: 321 123"},
	{"fldSeq", "RespHeader[2] eq 123", "x-test: 123 321",
		"x-test: 321 123"},
	{"fldSneq", "RespHeader[2] ne 123", "x-test: 321 123",
		"x-test: 123 321"},
}

var queryTxTestVec = []queryTestCase{
	{"and", `RespStatus == 200 and RespStatus ~ "^200$"`, "", ""},
	{"or", `RespStatus == 404 or RespStatus ~ "^200$"`, "", ""},
	{"not", `RespStatus == 404 or not RespStatus ~ "^404"`, "", ""},
	{"exprGrp",
		`(RespStatus == 200 or RespStatus == 404) and RespStatus == 200`,
		"", ""},
	{"andOr",
		`RespStatus == 200 or RespStatus == 503 and RespStatus == 404`,
		"", ""},
	{"tagList", `Debug,Resp* == 200`, "", ""},
	{"prefix", `Resp*:x-test eq "123 321"`, "", ""},
	{"noRHS", `RespStatus`, "", ""},
	{"lvlEq", `{1}Begin ~ req`, "", ""},
	{"lvlLeq", `{2-}Begin ~ req`, "", ""},
	{"lvlGeq", `{0+}Begin ~ req`, "", ""},
}

var queryVxidTestVec = []queryTestCase{
	{"vxidEq", "vxid == 118200424", "pass", ""},
	{"vxidNeq", "vxid != 118200424", "", "fail"},
	{"vxidLt", "vxid < 118200424", "", "fail"},
	{"vxidLeq", "vxid <= 118200424", "pass", ""},
	{"vxidGt", "vxid > 118200424", "", "fail"},
	{"vxidGeq", "vxid >= 118200424", "pass", ""},
}

func TestTestVXID(t *testing.T) {
	testTx := Tx{VXID: 118200424}
	for _, v := range queryVxidTestVec {
		t.Run(v.name, func(t *testing.T) {
			expr, err := parseQuery(v.query)
			if err != nil {
				t.Fatal("parseQuery():", err)
				return
			}
			if v.pass != "" && !expr.testVXID(testTx) {
				t.Errorf("query='%s' testVXID() not true as "+
					"expected", v.query)
			}
			if v.fail != "" && expr.testVXID(testTx) {
				t.Errorf("query='%s' testVXID() not false as "+
					"expected", v.query)
			}
		})
	}
}

func TestTestRecord(t *testing.T) {
	for _, v := range queryRecTestVec {
		t.Run(v.name, func(t *testing.T) {
			expr, err := parseQuery(v.query)
			if err != nil {
				t.Fatal("parseQuery():", err)
				return
			}

			passRec := Record{Payload: []byte(v.pass)}
			failRec := Record{Payload: []byte(v.fail)}
			if !expr.testRecord(passRec) {
				t.Errorf("query='%s' testRecord(%s) did not "+
					"succeed as expected", v.query, v.pass)
			}
			if expr.testRecord(failRec) {
				t.Errorf("query='%s' testRecord(%s) did not "+
					"fail as expected", v.query, v.fail)
			}
		})
	}
}

func BenchmarkTestRecord(b *testing.B) {
	for _, v := range queryRecTestVec {
		b.Run(v.name, func(b *testing.B) {
			expr, err := parseQuery(v.query)
			if err != nil {
				b.Fatal("parseQuery():", err)
				return
			}

			rec := Record{Payload: []byte(v.pass)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				expr.testRecord(rec)
			}
		})
	}
}

func TestEvalQuery(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(l1Log)
	if err != nil {
		t.Fatal("NewCursorFile(l00001.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(VXID, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	txGrp, status := q.NextTxGroup()
	if status != More {
		t.Fatalf("NextTxGrp() status want=Mode got=%v", status)
	}

	for _, v := range queryRecTestVec {
		t.Run(v.name, func(t *testing.T) {
			expr, err := parseQuery(v.query)
			if err != nil {
				t.Fatal("parseQuery():", err)
				return
			}
			if !expr.eval(txGrp) {
				t.Error("eval(txGrp) want=true got=false")
			}
		})
	}
	for _, v := range queryTxTestVec {
		t.Run(v.name, func(t *testing.T) {
			expr, err := parseQuery(v.query)
			if err != nil {
				t.Fatal("parseQuery():", err)
				return
			}
			if !expr.eval(txGrp) {
				t.Error("eval(txGrp) want=true got=false")
			}
		})
	}
	for _, v := range queryVxidTestVec {
		t.Run(v.name, func(t *testing.T) {
			expr, err := parseQuery(v.query)
			if err != nil {
				t.Fatal("parseQuery():", err)
				return
			}
			if v.pass != "" && !expr.eval(txGrp) {
				t.Errorf("query='%s' eval(txGrp) not true as "+
					"expected", v.query)
			}
			if v.fail != "" && expr.eval(txGrp) {
				t.Errorf("query='%s' eval(txGrp) not false as "+
					"expected", v.query)
			}
		})
	}
}

func BenchmarkEvalQuery(b *testing.B) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(l1Log)
	if err != nil {
		b.Fatal("NewCursorFile(l00001.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(VXID, "")
	if err != nil {
		b.Fatal("NewQuery():", err)
		return
	}

	txGrp, status := q.NextTxGroup()
	if status != More {
		b.Fatalf("NextTxGrp() status want=Mode got=%v", status)
	}

	for _, v := range queryRecTestVec {
		b.Run(v.name, func(b *testing.B) {
			expr, err := parseQuery(v.query)
			if err != nil {
				b.Fatal("parseQuery():", err)
				return
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				expr.eval(txGrp)
			}
		})
	}
	for _, v := range queryTxTestVec {
		b.Run(v.name, func(b *testing.B) {
			expr, err := parseQuery(v.query)
			if err != nil {
				b.Fatal("parseQuery():", err)
				return
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				expr.eval(txGrp)
			}
		})
	}
	for _, v := range queryVxidTestVec {
		b.Run(v.name, func(b *testing.B) {
			expr, err := parseQuery(v.query)
			if err != nil {
				b.Fatal("parseQuery():", err)
				return
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				expr.eval(txGrp)
			}
		})
	}
}
