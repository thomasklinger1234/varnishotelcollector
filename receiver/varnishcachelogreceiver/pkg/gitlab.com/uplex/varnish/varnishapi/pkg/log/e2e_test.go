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
	"testing"
	"time"
)

func TestAttach(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}

	var n *Log
	if err := n.Attach(""); err == nil {
		t.Error("expected nil.Attach() to fail")
	}
	uninit := new(Log)
	if err := uninit.Attach(""); err == nil {
		t.Error("expected uninitialized.Attach() to fail")
	}
}

const zerosec = time.Duration(0)

func TestTimeout(t *testing.T) {
	l := New()
	defer l.Release()
	for _, s := range []int{0, 1, -1, ^0, int(^uint(0) >> 1)} {
		if e := l.Timeout(time.Duration(s) * time.Second); e != nil {
			t.Error("Timeout():", e)
		}
	}

	var n *Log
	if err := n.Timeout(zerosec); err == nil {
		t.Error("expected nil.Timeout() to fail")
	}
	uninit := new(Log)
	if err := uninit.Timeout(zerosec); err == nil {
		t.Error("expected uninitialized.Timeout() to fail")
	}
}

func TestAttachInstance(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach("gotest"); err != nil {
		t.Error("AttachInstance(\"gotest\"):", err)
	}

	f := New()
	defer f.Release()
	if err := f.Timeout(zerosec); err != nil {
		t.Error("Timeout(0s):", err)
	}
	if err := f.Attach("instanceDoesNotExist"); err == nil {
		t.Error("expected Attach() to fail for a " +
			"non-existent instance")
	}
}
