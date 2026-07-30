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

func TestTags(t *testing.T) {
	for i, tag := range Tags {
		if Tag(i).String() != tag.String {
			t.Errorf("Tag %d string mismatch .String()=%v "+
				"Tags[%d].String=%v", i, Tag(i).String(),
				i, Tags[i].String)
		}
		if !tag.Legal && tag.String != "" {
			t.Errorf("Non-empty String for illegal Tag %d: %s", i,
				tag.String)
		}
	}
}

var expTxTypeStr = map[TxType]string{
	TxUnknown: "Unknown",
	Sess:      "Session",
	Req:       "Request",
	BeReq:     "BeReq",
	TxRaw:     "Record",
}

func TestTxTypeString(t *testing.T) {
	for k, v := range expTxTypeStr {
		if k.String() != v {
			t.Errorf("%d.String() expected=%v got=%v", k, v,
				k.String())
		}
	}
}

var expReasonStr = map[Reason]string{
	ReasonUnknown: "unknown",
	HTTP1:         "HTTP/1",
	RxReq:         "rxreq",
	ESI:           "esi",
	Restart:       "restart",
	Pass:          "pass",
	Fetch:         "fetch",
	BgFetch:       "bgfetch",
	Pipe:          "pipe",
}

func TestReasonString(t *testing.T) {
	for k, v := range expReasonStr {
		if k.String() != v {
			t.Errorf("%d.String() expected=%v got=%v", k, v,
				k.String())
		}
	}
}

func TestStatusString(t *testing.T) {
	// Just make sure it doesn't panic, don't want tests to depend
	// on specific values of the strings.
	stati := [...]Status{
		WriteErr, IOErr, Overrun, Abandoned, EOF, EOL, More,
	}
	for _, v := range stati {
		_ = v.String()
		_ = v.Error()
	}
}

var expRecordTypeStr = map[RecordType]string{
	Client:  "c",
	Backend: "b",
	None:    "-",
}

func TestRecordTypeString(t *testing.T) {
	for k, v := range expRecordTypeStr {
		if k.String() != v {
			t.Errorf("%c.String() expected=%v got=%v", k, v,
				k.String())
		}
	}
}

func TestNewRelease(t *testing.T) {
	l := New()
	defer l.Release()
	if l == nil {
		t.Fatal("New() returned nil")
	}

	// don't panic if the Log is nil or uninitialized
	var n *Log
	n.Release()
	uninit := &Log{}
	uninit.Release()
}

func TestError(t *testing.T) {
	// just make sure there is no panic for the nil or "no error" cases
	var n *Log
	_ = n.Error()
	uninit := &Log{}
	_ = uninit.Error()

	l := New()
	defer l.Release()
	_ = l.Error()
}
