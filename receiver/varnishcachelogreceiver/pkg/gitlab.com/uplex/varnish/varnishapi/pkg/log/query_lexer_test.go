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

type expLex struct {
	query  string
	tokens []qToken
}

// Examples from vsl-query(7)
var expLexn = []expLex{
	{
		query: `ReqURL eq "/foo"`,
		tokens: []qToken{
			{tokType: val, val: "ReqURL"},
			{tokType: seq},
			{tokType: val, val: "/foo"},
		},
	},
	{
		query: `ReqHeader:cookie`,
		tokens: []qToken{
			{tokType: val, val: "ReqHeader"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "cookie"},
		},
	},
	{
		query: `not ReqHeader:cookie`,
		tokens: []qToken{
			{tokType: not},
			{tokType: val, val: "ReqHeader"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "cookie"},
		},
	},
	{
		query: `Timestamp:Process[2] > 0.8`,
		tokens: []qToken{
			{tokType: val, val: "Timestamp"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "Process"},
			{tokType: char, scanType: '['},
			{tokType: val, val: "2"},
			{tokType: char, scanType: ']'},
			{tokType: gt},
			{tokType: val, val: "0.8"},
		},
	},
	{
		query: `ReqHeader:user-agent ~ "iPod" and Timestamp:Resp[2] > 1.`,
		tokens: []qToken{
			{tokType: val, val: "ReqHeader"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "user-agent"},
			{tokType: match},
			{tokType: val, val: "iPod"},
			{tokType: and},
			{tokType: val, val: "Timestamp"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "Resp"},
			{tokType: char, scanType: '['},
			{tokType: val, val: "2"},
			{tokType: char, scanType: ']'},
			{tokType: gt},
			{tokType: val, val: "1."},
		},
	},
	{
		query: "BerespStatus >= 500",
		tokens: []qToken{
			{tokType: val, val: "BerespStatus"},
			{tokType: geq},
			{tokType: val, val: "500"},
		},
	},
	{
		query: "ReqStatus == 304 and not ReqHeader:if-modified-since",
		tokens: []qToken{
			{tokType: val, val: "ReqStatus"},
			{tokType: eq},
			{tokType: val, val: "304"},
			{tokType: and},
			{tokType: not},
			{tokType: val, val: "ReqHeader"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "if-modified-since"},
		},
	},
	{
		query: "BerespStatus >= 500 or {2+}Timestamp:Process[2] > 1.",
		tokens: []qToken{
			{tokType: val, val: "BerespStatus"},
			{tokType: geq},
			{tokType: val, val: "500"},
			{tokType: or},
			{tokType: char, scanType: '{'},
			{tokType: val, val: "2+"},
			{tokType: char, scanType: '}'},
			{tokType: val, val: "Timestamp"},
			{tokType: char, scanType: ':'},
			{tokType: val, val: "Process"},
			{tokType: char, scanType: '['},
			{tokType: val, val: "2"},
			{tokType: char, scanType: ']'},
			{tokType: gt},
			{tokType: val, val: "1."},
		},
	},
	{
		query: "vxid == 0 and Error",
		tokens: []qToken{
			{tokType: vxid},
			{tokType: eq},
			{tokType: val, val: "0"},
			{tokType: and},
			{tokType: val, val: "Error"},
		},
	},
}

func TestLexer(t *testing.T) {
	for _, expLex := range expLexn {
		lexer := newLexer(expLex.query)
		expTokens := expLex.tokens
		for i, expTok := range expTokens {
			tok, err := lexer.nextToken()
			if err != nil {
				t.Fatal("nextToken():", err)
				return
			}
			if tok.tokType != expTok.tokType {
				t.Errorf("query=%q tok=%d tokType want=%v "+
					"got=%v",
					expLex.query, i, expTok.tokType,
					tok.tokType)
			}
			if expTok.tokType == val && tok.val != expTok.val {
				t.Errorf("val want=%v got=%v", expTok.val,
					tok.tokType)
			}
			if expTok.tokType == char &&
				tok.scanType != expTok.scanType {
				t.Errorf("scanType want=%v got=%v",
					expTok.scanType, tok.scanType)
			}
		}
		tok, err := lexer.nextToken()
		if err != nil {
			t.Error("nextToken():", err)
		}
		if tok.tokType != eoi {
			t.Errorf("tokType want=eoi got=%v", tok.tokType)
		}
	}
}
