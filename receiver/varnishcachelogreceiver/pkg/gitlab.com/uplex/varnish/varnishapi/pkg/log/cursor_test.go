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

func TestNewCursorFile(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
	}
	defer c.Delete()

	if _, err := l.NewCursorFile(failPath); err == nil {
		t.Error("Expected NewCursorFile(nonexistent) to fail")
	}

	var n *Log
	if _, err := n.NewCursorFile(testFile); err == nil {
		t.Error("Expected nil.NewCursorFile() to fail")
	}
	uninit := &Log{}
	if _, err := uninit.NewCursorFile(testFile); err == nil {
		t.Error("Expected uninitiailized.NewCursorFile() to fail")
	}
}

func TestDelete(t *testing.T) {
	// don't panic if the Cursor is nil or uninitialized
	var c *Cursor
	c.Delete()
	uninit := &Cursor{}
	uninit.Delete()
}

func TestNext(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	status := c.Next()
	// XXX change the status constant, this is vcl_more
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
}

var expFirstRec = testRec{
	vxid:    0,
	tag:     "Backend_health",
	rectype: '-',
	payload: "boot.test Still healthy 4---X-RH 1 1 1 0.000402 0.000484 HTTP/1.1 204 No Content",
}

func TestTag(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	status := c.Next()
	// XXX
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
	tag := c.Tag()
	if tag.String() != expFirstRec.tag {
		t.Errorf("Tag() want=%v got=%v\n", expFirstRec.tag,
			tag.String())
	}
}

func TestVXID(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	status := c.Next()
	// XXX
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
	vxid := c.VXID()
	if vxid != uint32(expFirstRec.vxid) {
		t.Errorf("VXID() want=%v got=%v\n", expFirstRec.vxid, vxid)
	}
}

func TestBackend(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	status := c.Next()
	// XXX
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
	backend := c.Backend()
	if backend != (expFirstRec.rectype == rune(Backend)) {
		t.Errorf("Backend() want=%v got=%v\n",
			expFirstRec.rectype == rune(Backend), backend)
	}
}

func TestClient(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
	}
	defer c.Delete()

	status := c.Next()
	// XXX
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
	client := c.Client()
	if client != (expFirstRec.rectype == rune(Client)) {
		t.Errorf("Client() want=%v got=%v\n",
			expFirstRec.rectype == rune(Client), client)
	}
}

func TestPayload(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	status := c.Next()
	// XXX
	if status != More {
		t.Errorf("Next() status want=More got=%d\n", uint8(status))
	}
	payload := c.Payload()
	if string(payload) != expFirstRec.payload {
		t.Errorf("Payload() want=%v got=%v\n", expFirstRec.payload,
			payload)
	}
}

func TestNextFileAll(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()

	for _, rec := range expRawLog {
		status := c.Next()
		if status == EOF {
			return
		}
		// XXX should be More
		if status != More {
			t.Fatal("Next() unexpected status:", status)
			return
		}
		vxid := c.VXID()
		if vxid != uint32(rec.vxid) {
			t.Errorf("VXID want=%v got=%v\n", rec.vxid, vxid)
		}
		tag := c.Tag()
		if tag.String() != rec.tag {
			t.Errorf("Tag() want=%v got=%v\n", rec.tag,
				tag.String())
		}
		payload := c.Payload()
		if string(payload) != rec.payload {
			t.Errorf("Payload() want=%v got=%v\n", rec.payload,
				string(payload))
		}
		backend := c.Backend()
		if backend && rec.rectype != 'b' {
			t.Errorf("Backend() want=%v got=b\n", rec.rectype)
		}
		client := c.Client()
		if client && rec.rectype != 'c' {
			t.Errorf("Client() want=%v got=c\n", rec.rectype)
		}
	}
}

func TestNextFileConcurrent(t *testing.T) {
	l := New()
	defer l.Release()

	c1, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c1.Delete()

	c2, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c2.Delete()

	for _, rec := range expRawLog {
		status1 := c1.Next()
		status2 := c2.Next()
		if status1 != status2 {
			t.Errorf("Next() differing status: %v %v\n", status1,
				status2)
		}
		if status1 == EOF {
			return
		}
		// XXX
		if status1 != More {
			t.Fatal("Next() unexpected status:", status1)
			return
		}
		cursors := [2]*Cursor{c1, c2}
		for _, c := range cursors {
			vxid := c.VXID()
			if vxid != uint32(rec.vxid) {
				t.Errorf("VXID want=%v got=%v\n", rec.vxid,
					vxid)
			}
			tag := c.Tag()
			if tag.String() != rec.tag {
				t.Errorf("Tag() want=%v got=%v\n", rec.tag,
					tag.String())
			}
			payload := c.Payload()
			if string(payload) != rec.payload {
				t.Errorf("Payload() want=%v got=%v\n",
					rec.payload, string(payload))
			}
			backend := c.Backend()
			if backend && rec.rectype != 'b' {
				t.Errorf("Backend() want=%v got=b\n",
					rec.rectype)
			}
			client := c.Client()
			if client && rec.rectype != 'c' {
				t.Errorf("Client() want=%v got=c\n",
					rec.rectype)
			}
		}
	}
}

func BenchmarkNextFileOne(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		b.ReportAllocs()

		l := New()
		defer l.Release()

		c, err := l.NewCursorFile(testFile)
		if err != nil {
			b.Fatal("NewCursorFile(bin.log):", err)
		}

		b.StartTimer()
		status := c.Next()
		b.StopTimer()
		if status != More {
			b.Fatalf("Next() status want=More got=%d\n",
				uint8(status))
		}
		c.Delete()
	}
}
