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

package admin

import (
	"gitlab.com/uplex/varnish/varnishapi/pkg/vsm"

	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	address string
	secret  []byte
)

func TestMain(m *testing.M) {
	vsm := vsm.New()
	if vsm == nil {
		panic("Cannot open VSM")
	}
	defer vsm.Destroy()
	if err := vsm.Attach(""); err != nil {
		panic(err)
	}
	addr, err := vsm.GetMgmtAddr()
	if err != nil {
		panic(err)
	}
	address = addr

	sarg, err := vsm.GetSecretPath()
	if err != nil {
		panic(err)
	}

	sfile, err := os.Open(sarg)
	if err != nil {
		panic(err)
	}
	secret, err = ioutil.ReadAll(sfile)
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func TestDial(t *testing.T) {
	adm, err := Dial(address, secret, time.Duration(0))
	if err != nil {
		t.Fatal("Dial():", err)
		return
	}
	defer adm.Close()
	if adm.Banner == "" {
		t.Error("Banner is empty after Attach()")
	}
	if !strings.Contains(adm.Banner, "Varnish Cache CLI") {
		t.Error("Banner does not contain 'Varnish Cache CLI'")
	}
}

func TestListen(t *testing.T) {
	adm, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal("Listen():", err)
		return
	}
	defer adm.Close()
}

func TestClose(t *testing.T) {
	adm, err := Dial(address, secret, time.Duration(0))
	if err != nil {
		t.Fatal("Dial():", err)
		return
	}
	if err = adm.Close(); err != nil {
		t.Error("Close():", err)
	}

	var n *Admin
	if err = n.Close(); err == nil {
		t.Error("Expected nil.Close() to fail")
	}
}

func TestLocalAddr(t *testing.T) {
	adm, err := Dial(address, secret, time.Duration(0))
	if err != nil {
		t.Fatal("Dial():", err)
		return
	}
	defer adm.Close()
	addr, err := adm.LocalAddr()
	if err != nil {
		t.Fatal("LocalAddr():", err)
		return
	}
	if addr.Network() != "tcp" {
		t.Errorf("LocalAddr().Network() want=tcp got=%v",
			addr.Network())
	}
	str := addr.String()
	if str == "" {
		t.Error("LocalAddr().String() is empty")
	}

	var n *Admin
	if addr, err = n.LocalAddr(); err == nil {
		t.Error("Expected nil.LocalAddr() to fail")
	}
	unconn := new(Admin)
	if addr, err = unconn.LocalAddr(); err == nil {
		t.Error("Expected unconnected.LocalAddr() to fail")
	}
}

func TestRemoteAddr(t *testing.T) {
	adm, err := Dial(address, secret, time.Duration(0))
	if err != nil {
		t.Fatal("Dial():", err)
		return
	}
	defer adm.Close()
	addr, err := adm.RemoteAddr()
	if err != nil {
		t.Fatal("RemoteAddr():", err)
		return
	}
	if addr.Network() != "tcp" {
		t.Errorf("RemoteAddr().Network() want=tcp got=%v",
			addr.Network())
	}
	str := addr.String()
	if str == "" {
		t.Error("RemoteAddr().String() is empty")
	}
	if str != address {
		t.Errorf("RemoteAddr().String() want=%v got=%v", address, str)
	}

	var n *Admin
	if addr, err = n.RemoteAddr(); err == nil {
		t.Error("Expected nil.RemoteAddr() to fail")
	}
	unconn := new(Admin)
	if addr, err = unconn.RemoteAddr(); err == nil {
		t.Error("Expected unconnected.RemoteAddr() to fail")
	}
}

func TestListenerAddr(t *testing.T) {
	adm, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal("Listen():", err)
		return
	}
	defer adm.Close()
	addr, err := adm.ListenerAddr()
	if err != nil {
		t.Fatal("ListenerAddr():", err)
		return
	}
	if addr.Network() != "tcp" {
		t.Errorf("ListenerAddr().Network() want=tcp got=%v",
			addr.Network())
	}
	str := addr.String()
	if str == "" {
		t.Error("ListenerAddr().String() is empty")
	}

	var n *Admin
	if addr, err = n.ListenerAddr(); err == nil {
		t.Error("Expected nil.ListenerAddr() to fail")
	}
	unconn := new(Admin)
	if addr, err = unconn.ListenerAddr(); err == nil {
		t.Error("Expected unconnected.ListenerAddr() to fail")
	}
}

func TestCommand(t *testing.T) {
	adm, err := Dial(address, secret, time.Duration(0))
	if err != nil {
		t.Fatal("Dial():", err)
		return
	}
	defer adm.Close()
	resp, err := adm.Command("banner")
	if err != nil {
		t.Fatal("Command():", err)
		return
	}
	if resp.Msg == "" {
		t.Error("resp.Msg after Command() is empty")
	}
	if resp.Msg != adm.Banner {
		t.Errorf("resp.Msg after Command(banner) want=%v got=%v",
			resp.Msg, adm.Banner)
	}
	if resp.Code < 100 || resp.Code > 999 {
		t.Errorf("Expected 100 <= resp.Code <= 999 got=%v", resp.Code)
	}
	for _, cmd := range []string{"ping", "status"} {
		resp, err := adm.Command(cmd)
		if err != nil {
			t.Fatalf("Command(%s): %v", cmd, err)
			return
		}
		if resp.Msg == "" {
			t.Errorf("resp.Msg after Command(%s) is empty", cmd)
		}
		if resp.Code < 100 || resp.Code > 999 {
			t.Errorf("Command(%s): Expected 100 <= resp.Code "+
				"<= 999 got=%v", cmd, resp.Code)
		}
	}

	var n *Admin
	if resp, err = n.Command("banner"); err == nil {
		t.Error("Expected nil.Command() to fail")
	}
	unconn := new(Admin)
	if resp, err = unconn.Command("banner"); err == nil {
		t.Error("Expected unconnected.Command() to fail")
	}
}
