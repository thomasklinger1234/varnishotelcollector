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
// option), and an instance named "gotest" (varnishd -n "gotest"). For
// the purposes of this test, it does not matter how either instance
// is configured, only that they are running.
package vsm

import (
	"testing"
	"time"
)

func TestNewDestroy(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v == nil {
		t.Fatal("New() returned nil")
	}

	var n *VSM
	if err := n.Destroy(); err == nil {
		t.Error("expected nil.Destroy() to fail")
	}
	uninit := new(VSM)
	if err := uninit.Destroy(); err == nil {
		t.Error("expected uninitialized.Destroy() to fail")
	}
}

const nilobjerr string = "VSM object is nil"
const uniniterr string = "VSM object was uninitialized via New()"

func TestError(t *testing.T) {
	v := New()
	defer v.Destroy()
	s := v.Error()
	if s == "" {
		t.Error("Error() expected empty got: ", s)
	}

	var n *VSM
	if err := n.Error(); err != nilobjerr {
		t.Errorf("nil.Error() expected=\"%v\" got=\"%v\"",
			nilobjerr, err)
	}
	uninit := new(VSM)
	if err := uninit.Error(); err != uniniterr {
		t.Errorf("uninitialized.Error() expected=\"%v\" got=\"%v\"",
			uniniterr, err)
	}
}

func TestAttach(t *testing.T) {
	v := New()
	defer v.Destroy()
	if err := v.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}

	var n *VSM
	if err := n.Attach(""); err == nil {
		t.Error("expected nil.Attach() to fail")
	}
	uninit := new(VSM)
	if err := uninit.Attach(""); err == nil {
		t.Error("expected uninitialized.Attach() to fail")
	}
}

const zerosec = time.Duration(0)

func TestTimeout(t *testing.T) {
	v := New()
	defer v.Destroy()
	for _, s := range []int{0, 1, -1, ^0, int(^uint(0) >> 1)} {
		if e := v.Timeout(time.Duration(s) * time.Second); e != nil {
			t.Error("Timeout():", e)
		}
	}

	var n *VSM
	if err := n.Timeout(zerosec); err == nil {
		t.Error("expected nil.Timeout() to fail")
	}
	uninit := new(VSM)
	if err := uninit.Timeout(zerosec); err == nil {
		t.Error("expected uninitialized.Timeout() to fail")
	}
}

func TestAttachInstance(t *testing.T) {
	v := New()
	defer v.Destroy()
	if err := v.Attach("gotest"); err != nil {
		t.Error("Attach(\"gotest\"):", err)
	}

	f := New()
	defer f.Destroy()
	if err := f.Timeout(zerosec); err != nil {
		t.Error("Timeout(0s):", err)
	}
	if err := f.Attach("instanceDoesNotExist"); err == nil {
		t.Error("expected Attach() to fail for a " +
			"non-existent instance")
	}
}

func TestPointer(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v.VSM == nil {
		t.Error("VSM handle is nil")
	}

	uninit := new(VSM)
	if uninit.VSM != nil {
		t.Error("expected uninitialized.VSM == nil")
	}
}

func TestStatus(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v.VSM == nil {
		t.Fatal("VSM handle is nil")
		return
	}

	if err := v.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	status, err := v.Status()
	if err != nil {
		t.Error("Status():", err)
	}
	vsmerr := v.Error()
	if vsmerr != "No VSM error" {
		t.Errorf("Error() after Status() want='No VSM Error' got='%v'",
			vsmerr)
	}

	unattached := New()
	defer unattached.Destroy()
	if unattached.VSM == nil {
		t.Fatal("VSM handle is nil")
		return
	}
	status, err = unattached.Status()
	if err != nil {
		t.Error("unattached.Status():", err)
	}
	if status.MgtRunning {
		t.Error("unattached.Status().MgtRunning == true")
	}
	if status.WrkRunning {
		t.Error("unattached.Status().WrkRunning == true")
	}
	vsmerr = unattached.Error()
	if vsmerr == "No VSM error" {
		t.Error("Error() after unattached.Status():", vsmerr)
	}

	var n *VSM
	if status, err = n.Status(); err == nil {
		t.Error("expected nil.Status() to fail")
	}
	uninit := new(VSM)
	if status, err = uninit.Status(); err == nil {
		t.Error("expected uninitialized.Status() to fail")
	}
}

func TestGet(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v.VSM == nil {
		t.Fatal("VSM handle is nil")
		return
	}

	if err := v.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	targ, err := v.Get("Arg", "-T")
	if err != nil {
		t.Error("Get():", err)
	}
	if targ == "" {
		t.Error("Got empty string for Get(Arg, -T)")
	}
	sarg, err := v.Get("Arg", "-S")
	if err != nil {
		t.Error("Get():", err)
	}
	if sarg == "" {
		t.Error("Got empty string for Get(Arg, -S)")
	}
	foobar, err := v.Get("foo", "bar")
	if err == nil {
		t.Error("Expected error for Get(foo, bar)")
	}
	if foobar != "" {
		t.Error("Expected empty string for Get(foo, bar), got:", foobar)
	}

	var n *VSM
	if _, err = n.Get("foo", "bar"); err == nil {
		t.Error("expected nil.Get() to fail")
	}
	uninit := new(VSM)
	if _, err = uninit.Get("foo", "bar"); err == nil {
		t.Error("expected uninitialized.Status() to fail")
	}
}

func TestGetMgmtAddr(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v.VSM == nil {
		t.Fatal("VSM handle is nil")
		return
	}

	if err := v.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	addr, err := v.GetMgmtAddr()
	if err != nil {
		t.Error("GetMgmtAddr():", err)
	}
	if addr == "" {
		t.Error("Got empty string for GetMgmtAddr()")
	}

	var n *VSM
	if _, err = n.GetMgmtAddr(); err == nil {
		t.Error("expected nil.GetMgmtAddr() to fail")
	}
	uninit := new(VSM)
	if _, err = uninit.GetMgmtAddr(); err == nil {
		t.Error("expected uninitialized.GetMgmtAddr() to fail")
	}
}

func TestGetSecretPath(t *testing.T) {
	v := New()
	defer v.Destroy()
	if v.VSM == nil {
		t.Fatal("VSM handle is nil")
		return
	}

	if err := v.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	path, err := v.GetSecretPath()
	if err != nil {
		t.Error("GetSecretPath():", err)
	}
	if path == "" {
		t.Error("Got empty string for GetSecretPath()")
	}

	var n *VSM
	if _, err = n.GetSecretPath(); err == nil {
		t.Error("expected nil.GetSecretPath() to fail")
	}
	uninit := new(VSM)
	if _, err = uninit.GetSecretPath(); err == nil {
		t.Error("expected uninitialized.GetSecretPath() to fail")
	}
}
