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
package stats

import (
	"strings"
	"testing"
	"time"
)

type expRuneStr struct {
	c   rune
	str string
}

const zerosec = time.Duration(0)

var (
	expStats = [...]string{"MGT.uptime", "MAIN.uptime", "MAIN.n_vcl"}
	errStats = [...]string{"foo", "bar", "baz", "quux"}
	s7sn     = map[Semantics]expRuneStr{
		Counter:    {'c', "counter"},
		Gauge:      {'g', "gauge"},
		S7sBitmap:  {'b', "bitmap"},
		S7sUnknown: {'?', "unknown"},
	}
	formats = map[Format]expRuneStr{
		Integer:       {'i', "integer"},
		Bytes:         {'B', "bytes"},
		FormatBitmap:  {'b', "bitmap"},
		Duration:      {'d', "duration"},
		FormatUnknown: {'?', "unknown"},
	}
)

func TestSemantics(t *testing.T) {
	for s, exp := range s7sn {
		if rune(s) != exp.c {
			t.Errorf("rune(%v) want=%v got=%v", s, exp.c, rune(s))
		}
		if s.String() != exp.str {
			t.Errorf("%v.String() want=%v got=%v", s, exp.str,
				s.String())
		}
	}
}

func TestFormat(t *testing.T) {
	for f, exp := range formats {
		if rune(f) != exp.c {
			t.Errorf("rune(%v) want=%v got=%v", f, exp.c, rune(f))
		}
		if f.String() != exp.str {
			t.Errorf("%v.String() want=%v got=%v", f, exp.str,
				f.String())
		}
	}
}

func TestNewRelease(t *testing.T) {
	s := New()
	defer s.Release()
	if s == nil {
		t.Fatal("New() returned nil")
	}

	// don't panic if the Stats is nil or uninitialized
	var n *Stats
	n.Release()
	uninit := &Stats{}
	uninit.Release()
}

func TestAttach(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	var n *Stats
	if err := n.Attach(""); err == nil {
		t.Error("expected nil.Attach() to fail")
	}
	uninit := new(Stats)
	if err := uninit.Attach(""); err == nil {
		t.Error("expected uninitialized.Attach() to fail")
	}
}

func TestTimeout(t *testing.T) {
	s := New()
	defer s.Release()
	for _, d := range []int{0, 1, -1, ^0, int(^uint(0) >> 1)} {
		if e := s.Timeout(time.Duration(d) * time.Second); e != nil {
			t.Error("Timeout():", e)
		}
	}

	var n *Stats
	if err := n.Timeout(zerosec); err == nil {
		t.Error("expected nil.Timeout() to fail")
	}
	uninit := new(Stats)
	if err := uninit.Timeout(zerosec); err == nil {
		t.Error("expected uninitialized.Timeout() to fail")
	}
}

func TestAttachInstance(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Attach("gotest"); err != nil {
		t.Error("Attach(\"gotest\"):", err)
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

func TestLevels(t *testing.T) {
	if len(Levels) == 0 {
		t.Fatal("Levels is empty")
		return
	}
	for _, l := range Levels {
		if l.Label == "" {
			t.Error("Level.Label is emtpy:", l)
		}
		if l.Label != l.String() {
			t.Errorf("Level.Label field=%v String()=%v", l.Label,
				l.String())
		}
		if l.ShortD9n == "" {
			t.Error("Level.ShortD9n is empty:", l)
		}
		if l.LongD9n == "" {
			t.Error("Level.LongD9n is empty:", l)
		}
	}
}

func TestD9ns(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}

	for _, v := range expStats {
		d9n, err := s.D9n(v)
		if err != nil {
			t.Errorf("description for %v: %v", v, err)
			return
		}
		if d9n.ShortD9n == "" {
			t.Error("D9n.ShortD9n is empty:", d9n)
		}
		if d9n.ShortD9n != d9n.String() {
			t.Errorf("D9n.ShortD9n field=%v String()=%v",
				d9n.ShortD9n, d9n.String())
		}
		if d9n.Level == nil {
			t.Error("D9n.Level is nil:", d9n)
		}

		found := false
		for _, l := range Levels {
			if l.Label == d9n.Level.Label {
				found = true
				break
			}
		}
		if !found {
			t.Error("D9n.Level not found in Levels:", d9n)
		}

		found = false
		for s7s, _ := range s7sn {
			if d9n.Semantics == s7s {
				found = true
				break
			}
		}
		if !found {
			t.Error("D9n.Semantics not defined:", d9n)
		}

		found = false
		for f, _ := range formats {
			if d9n.Format == f {
				found = true
				break
			}
		}
		if !found {
			t.Error("D9n.Format not defined:", d9n)
		}
	}

	for _, v := range errStats {
		if _, err := s.D9n(v); err == nil {
			t.Errorf("expected error for s.D9n(%v)", v)
		}
	}

	var n *Stats
	if _, err := n.D9n(expStats[0]); err == nil {
		t.Error("expected nil.D9n() to fail")
	}
	uninit := new(Stats)
	if _, err := uninit.D9n(expStats[0]); err == nil {
		t.Error("expected uninitialized.D9n() to fail")
	}
	unattached := New()
	defer unattached.Release()
	if _, err := unattached.D9n(expStats[0]); err == nil {
		t.Error("expected D9n() to fail for unattached instance")
	}
}

func TestNames(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}
	names, err := s.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	if names == nil {
		t.Fatal("Names() returned nil")
		return
	}
	if len(names) == 0 {
		t.Fatal("Names() returned empty slice")
		return
	}
	var found [len(expStats)]bool
	for _, name := range names {
		if _, err := s.D9n(name); err != nil {
			t.Errorf("Stats.D9n(%v): %v", name, err)
		}
		for i, expStat := range expStats {
			if name == expStat {
				found[i] = true
				break
			}
		}
	}
	for name, f := range found {
		if !f {
			t.Errorf("%v not found in Stats.Names()", name)
		}
	}

	var n *Stats
	if _, err := n.Names(); err == nil {
		t.Error("expected nil.Names() to fail")
	}
	uninit := new(Stats)
	if _, err := uninit.Names(); err == nil {
		t.Error("expected uninitialized.Names() to fail")
	}
	unattached := New()
	defer unattached.Release()
	if _, err := unattached.Names(); err == nil {
		t.Error("expected Names() to fail for unattached instance")
	}
}

func TestInclude(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Include("MGT.*"); err != nil {
		t.Fatal("Include(MGT.*):", err)
		return
	}
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	names, err := s.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "MGT.") {
			t.Errorf("Names() returns %v after Include(MGT.*)",
				name)
		}
	}

	s2 := New()
	defer s2.Release()
	if err := s2.Include("MAIN.*"); err != nil {
		t.Fatal("Include(MAIN.*):", err)
		return
	}
	if err := s2.Include("MGT.uptime"); err != nil {
		t.Fatal("Include(MGT.uptime):", err)
		return
	}
	if err := s2.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	names, err = s2.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "MAIN.") {
			if name == "MGT.uptime" {
				continue
			}
			t.Errorf("Names() returns %v after Include(MAIN.*) "+
				"and Include(MGT.uptime)", name)
		}
	}

	fld := "MGT.uptime"
	var n *Stats
	if err := n.Include(fld); err == nil {
		t.Error("expected nil.Include() to fail")
	}
	uninit := new(Stats)
	if err := uninit.Include(fld); err == nil {
		t.Error("expected uninitialized.Include() to fail")
	}
	attached := New()
	defer attached.Release()
	if err := attached.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}
	if err := attached.Include(fld); err == nil {
		t.Error("expected Include() to fail for attached instance")
	}
}

func TestExclude(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Exclude("MGT.*"); err != nil {
		t.Fatal("Exclude(MGT.*):", err)
		return
	}
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	names, err := s.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	for _, name := range names {
		if strings.HasPrefix(name, "MGT.") {
			t.Errorf("Names() returns %v after Exclude(MGT.*)",
				name)
		}
	}

	s2 := New()
	defer s2.Release()
	if err := s2.Exclude("MAIN.*"); err != nil {
		t.Fatal("Exclude(MAIN.*):", err)
		return
	}
	if err := s2.Exclude("MGT.uptime"); err != nil {
		t.Fatal("Exclude(MGT.uptime):", err)
		return
	}
	if err := s2.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	names, err = s2.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	for _, name := range names {
		if strings.HasPrefix(name, "MAIN.") || name == "MGT.uptime" {
			t.Errorf("Names() returns %v after Exclude(MAIN.*) "+
				"and Exclude(MGT.uptime)", name)
		}
	}

	fld := "MGT.uptime"
	var n *Stats
	if err := n.Exclude(fld); err == nil {
		t.Error("expected nil.Exclude() to fail")
	}
	uninit := new(Stats)
	if err := uninit.Exclude(fld); err == nil {
		t.Error("expected uninitialized.Exclude() to fail")
	}
	attached := New()
	defer attached.Release()
	if err := attached.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}
	if err := attached.Exclude(fld); err == nil {
		t.Error("expected Exclude() to fail for attached instance")
	}
}

func TestInExclude(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Include("MAIN.*"); err != nil {
		t.Fatal("Include(MAIN.*):", err)
		return
	}
	if err := s.Exclude("MAIN.client_req"); err != nil {
		t.Fatal("Exclude(MGT.*):", err)
		return
	}
	if err := s.Include("MGT.uptime"); err != nil {
		t.Fatal("Include(MAIN.*):", err)
		return
	}
	if err := s.Exclude("MAIN.cache_hit"); err != nil {
		t.Fatal("Exclude(MGT.*):", err)
		return
	}
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}

	names, err := s.Names()
	if err != nil {
		t.Fatal("Names():", err)
		return
	}
	for _, name := range names {
		if (!strings.HasPrefix(name, "MAIN.") && name != "MGT.uptime") ||
			name == "MAIN.client_req" || name == "MAIN.cache_hit" {
			t.Errorf("Names() returns %v after Includes & Excludes",
				name)
		}
	}
}

func TestRead(t *testing.T) {
	rdHndlr := func(name string, val uint64) bool {
		return true
	}

	var n *Stats
	if err := n.Read(rdHndlr); err == nil {
		t.Error("expected nil.Read() to fail")
	}
	uninit := new(Stats)
	if err := uninit.Read(rdHndlr); err == nil {
		t.Error("expected uninitialized.Read() to fail")
	}
	unattached := New()
	defer unattached.Release()
	if err := unattached.Read(rdHndlr); err == nil {
		t.Error("expected Read() to fail for unattached instance")
	}

	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}
	if err := s.Read(nil); err == nil {
		t.Error("expected Read(nil) to fail")
	}
}

func TestE2EDefaultRead(t *testing.T) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
	}

	var stats = make(map[string]uint64)
	rdHndlr := func(name string, val uint64) bool {
		stats[name] = val
		return true
	}
	if err := s.Read(rdHndlr); err != nil {
		t.Fatal("Read():", err)
		return
	}

	if len(stats) == 0 {
		t.Error("Read() read no stats")
	}
	for _, v := range expStats {
		if _, ok := stats[v]; !ok {
			t.Error("no stats for", v)
		}
	}
	for _, v := range errStats {
		if _, ok := stats[v]; ok {
			t.Error("expected no such stat:", v)
		}
	}
	for stat, _ := range stats {
		if _, err := s.D9n(stat); err != nil {
			t.Errorf("description for stat %v: %v", stat, err)
		}
	}

	var nameMap = make(map[string]bool)
	names, err := s.Names()
	if err != nil {
		t.Fatal("Stats.Names():", err)
		return
	}
	if len(names) != len(stats) {
		t.Errorf("Stats.Read() number of stats want=%v got=%v",
			len(names), len(stats))
	}
	for _, name := range names {
		nameMap[name] = true
		if _, ok := stats[name]; !ok {
			t.Errorf("Stats.Read() did not read %v", name)
		}
	}
	for name, _ := range stats {
		if !nameMap[name] {
			t.Errorf("Stats.Read() included %v, not in Names()",
				name)
		}
	}

	var stats1 = make(map[string]uint64)
	stopHndlr := func(name string, val uint64) bool {
		stats1[name] = val
		return false
	}
	if err := s.Read(stopHndlr); err != nil {
		t.Fatal("Read(stopHndlr):", err)
		return
	}
	if nstats := len(stats1); nstats != 1 {
		t.Errorf("number of stats after stop handler want=1 got=%v",
			nstats)
	}
}

func BenchmarkReadOneStat(b *testing.B) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		b.Fatal("Attach(default):", err)
		return
	}

	names, err := s.Names()
	if err != nil {
		b.Fatal("Stats.Names():", err)
		return
	}
	// length of the stat name + 8 bytes for the uint64
	bytes := len(names[0]) + 8
	b.SetBytes(int64(bytes))

	rdHndlr := func(name string, val uint64) bool {
		return false
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Read(rdHndlr)
	}
}

func BenchmarkReadAllStats(b *testing.B) {
	s := New()
	defer s.Release()
	if err := s.Attach(""); err != nil {
		b.Fatal("Attach(default):", err)
		return
	}

	names, err := s.Names()
	if err != nil {
		b.Fatal("Stats.Names():", err)
		return
	}
	var bytes int64 = 0
	for _, name := range names {
		bytes += int64(len(name)) + 8
	}
	b.SetBytes(bytes)

	rdHndlr := func(name string, val uint64) bool {
		return true
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Read(rdHndlr)
	}
}
