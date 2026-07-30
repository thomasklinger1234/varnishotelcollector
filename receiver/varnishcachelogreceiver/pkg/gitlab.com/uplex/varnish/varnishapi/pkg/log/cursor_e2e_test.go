//go:build e2e
// +build e2e

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

// This test is only run if go test is invoked with a -tags option
// that includes "e2e". It requires that two instances of Varnish are
// running: a default instance (varnishd invoked without the -n
// option), and an instance named "gotest" (varnishd -n "gotest").
package log

import (
	"net/http"
	"testing"
)

func TestNewCursor(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	c, err := l.NewCursor()
	if err != nil {
		t.Fatal("NewCursor():", err)
		return
	}
	defer c.Delete()

	ln := New()
	defer ln.Release()
	if err := ln.Attach("gotest"); err != nil {
		t.Fatal("Attach(gotest):", err)
		return
	}

	cn, err := ln.NewCursor()
	if err != nil {
		t.Fatal("NewCursor() for gotest:", err)
		return
	}
	defer cn.Delete()
}

func TestIllegalCursorFile(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	c, err := l.NewCursorFile(testFile)
	if err == nil {
		t.Fatal("Expected NewCursorFile() to fail for attached Log")
		return
	}
	c.Delete()
}

func TestE2ECursor(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	c, err := l.NewCursor()
	if err != nil {
		t.Fatal("NewCursor():", err)
		return
	}
	defer c.Delete()

	client := &http.Client{}
	req, err := http.NewRequest("GET", url("/uncacheable"), nil)
	if err != nil {
		t.Fatal("/uncacheable Request", err)
	}
	req.Close = true
	stopChan := make(chan bool)
	respChan := make(chan bool)
	go func() {
		sent := false
		for {
			select {
			case <-stopChan:
				return
			default:
				_, err := client.Do(req)
				if err != nil {
					t.Fatal("GET request:", err)
					return
				}
				if !sent {
					respChan <- true
					sent = true
				}
			}
		}
	}()

	<-respChan
	var recs []testRec
	for i := 0; i < 100; i++ {
		status := c.Next()
		vxid := c.VXID()
		tag := c.Tag()
		payload := c.Payload()
		backend := c.Backend()
		client := c.Client()
		rec := testRec{
			vxid:    int(vxid),
			tag:     Tag(tag).String(),
			payload: string(payload),
		}
		if backend {
			rec.rectype = 'b'
		} else if client {
			rec.rectype = 'c'
		} else {
			rec.rectype = '-'
		}
		recs = append(recs, rec)
		if status == EOL {
			break
		}
	}
	stopChan <- true
	t.Logf("read %d records", len(recs))
	t.Log(recs)
}
