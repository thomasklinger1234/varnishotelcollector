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

// Package stats implements a client for the Varnish native statistics
// interface.
package stats

/*
#cgo pkg-config: varnishapi
#include "stats.h"
*/
import "C"

import (
	"gitlab.com/uplex/varnish/varnishapi/pkg/vsm"

	"errors"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Semantics classifies the type of a statistic, as part of a
// description (D9n).
//
// Just use rune(s) to get 'c', 'g', 'b' or '?'.
type Semantics uint8

const (
	// Counter indicates a stat that is monotone increasing during
	// the lifetime of the Varnish worker process.
	Counter Semantics = Semantics('c')

	// Gauge indicates a stat for the current level of a quantity,
	// which may rise or fall over time.
	Gauge = Semantics('g')

	// S7sBitmap indicates a bitmap ("S7s" for "Semantics") -- 64
	// yes/no states. It is commonly used for the ".happy"
	// statistic for a backend, with the results of the last 64
	// health checks.
	S7sBitmap = Semantics('b')

	// S7sUnknown will probably never appear in a stat
	// description.
	S7sUnknown = Semantics('?')
)

// String returns "counter", "gauge", "bitmap" or "unknown".
func (s Semantics) String() string {
	switch s {
	case Counter:
		return "counter"
	case Gauge:
		return "gauge"
	case S7sBitmap:
		return "bitmap"
	case S7sUnknown:
		return "unknown"
	default:
		return "invalid status"
	}
}

// Format indicates the display format suitable for the kind of data
// that a statistic represents.
//
// Just use rune(f) to get 'i', 'B', 'b', 'd' or '?'.
type Format uint8

const (
	// Integer format indicates that a stat is simply a number.
	Integer Format = Format('i')

	// Bytes may be displayed with SI notation, such as "4.7kB" or
	// "1.1MB".
	Bytes = Format('B')

	// FormatBitmap indicates display as a bitmap.
	FormatBitmap = Format('b')

	// Duration in seconds (not nanoseconds as in time.Duration),
	// which may be displayed as "47s" or "72h3m0s".
	Duration = Format('d')

	// FormatUnknown will probably never appear in a stat
	// description.
	FormatUnknown = Format('?')
)

// String returns "integer", "bytes", "bitmap", "duration" or
// "unknown".
func (f Format) String() string {
	switch f {
	case Integer:
		return "integer"
	case Bytes:
		return "bytes"
	case FormatBitmap:
		return "bitmap"
	case Duration:
		return "duration"
	case FormatUnknown:
		return "unknown"
	default:
		return "invalid status"
	}
}

// XXX implement fmt.Formatter interface for Format
// type Stat uint64

// func (s Stat) Format(f fmt.State, c rune) {
// 	switch Format(c) {
// 	case Integer:
// 		//
// 	case Bytes:
// 		//
// 	case FormatBitmap:
// 		//
// 	case Duration:
// 		//
// 	default:
// 		//
// 	}
// }

// Level is a classifier for the verbosity level at which a statistic
// may be displayed. For some time, Varnish has had the levels "INFO"
// for common stats (shown in the default view of varnishstat), "DIAG"
// for counters that are potentially useful for diagnosing problems,
// and "DEBUG" for counters from the innards of Varnish.
type Level struct {
	// Label is the name of the level ("INFO", "DIAG" and
	// "DEBUG").
	Label string

	// ShortD9n (for "short description") is a brief description
	// of the level.
	ShortD9n string

	// LongD9n is a long description of the level.
	LongD9n string
}

// String returns the level's Label.
func (v *Level) String() string {
	return v.Label
}

// D9n (for "description") is a set of information about a statistic.
// It is returned by Stats.D9n(name) for a given stat name.
type D9n struct {
	// ShortD9n is a brief description of a stat.
	ShortD9n string

	// LongD9n is a long description of a stat. May be empty.
	LongD9n string

	// Level indicates the stat's verbosity level.
	Level *Level

	// Semantics classifies the value of a stat.
	Semantics Semantics

	// Format indicates a suitable display format for a stat.
	Format Format
}

// String returns the short description.
func (d9n D9n) String() string {
	return d9n.ShortD9n
}

type statsData struct {
	d9ns  map[string]D9n
	names []string
}

var (
	// Levels is a []Level for the Levels in use by the Varnish
	// instance.
	Levels = setLvls()

	mapCtr  = uint64(0)
	cbMap   = make(map[uint64]ReadHandler)
	dataMap = make(map[uint64]*statsData)
	cbLock  sync.RWMutex
)

// A Stats instance is a client for the native statistics interface.
type Stats struct {
	vsc    *C.struct_vsc
	vsm    *vsm.VSM
	rdLock sync.Mutex
	names  []string
	d9ns   map[string]D9n
}

// New returns a new and initialized instance of Stats. Stats
// instances should only be created through this method, since it
// performs initializations for the native library that are required
// for the other methods.
//
// New may return nil in rare cases, usually in an out of memory
// condition.
func New() *Stats {
	stats := new(Stats)
	stats.vsc = C.VSC_New()
	if stats.vsc == nil {
		return nil
	}
	return stats
}

func (stats *Stats) checkNil() error {
	if stats == nil {
		return errors.New("Stats object is nil")
	}
	if stats.vsc == nil {
		return errors.New("Stats object was not initialized via New()")
	}
	return nil
}

func (stats *Stats) initVSM() error {
	if err := stats.checkNil(); err != nil {
		return err
	}
	if stats.vsm == nil {
		stats.vsm = vsm.New()
		if stats.vsm == nil {
			return errors.New("Cannot initialize for attachment " +
				"to shared memory")
		}
	}
	return nil
}

// Timeout sets the timeout for attaching to a Varnish instance.  The
// timeout tmo is truncated to seconds. If the truncated timeout >=
// 0s, then wait that many seconds to attach. If tmo < 0s, wait
// indefinitely. By default, the Varnish default timeout holds (5s in
// recent Varnish versions).
func (stats *Stats) Timeout(tmo time.Duration) error {
	if err := stats.initVSM(); err != nil {
		return err
	}
	return stats.vsm.Timeout(tmo)
}

func (stats *Stats) clude(nmGlob string, exclude bool) error {
	if err := stats.checkNil(); err != nil {
		return err
	}
	if stats.d9ns != nil || stats.names != nil {
		return errors.New("already attached to a Varnish instance")
	}
	if err := stats.initVSM(); err != nil {
		return err
	}
	if exclude {
		nmGlob = "^" + nmGlob
	}
	if status := C.VSC_Arg(stats.vsc, 'f', C.CString(nmGlob)); status != 1 {
		if status == -1 {
			return stats.vsm
		}
		panic("unexpected error from VSC_Arg(): " + string(status))
	}
	return nil
}

// Include restricts reads to statistics whose names match the pattern
// in nmGlob, which may include wildcards. For example, the argument
// may be "MAIN.client_req" to only include that stat, or "MAIN.*" to
// include all MAIN stats.
//
// Include and Exclude must be called before attaching to a Varnish
// instance. After an attach, the results of all further methods
// reflect the filtering, such Read, Names and D9n.
//
// Include and Exclude may be called more than once, and the inclusion
// and exclusion sets accumulate. For example, if Include is called
// with both "MAIN.*" and "MGT.uptime", then MGT.uptime and all of the
// MAIN stats will be included in all further reads. If in addition
// Exclude is called with "MAIN.client_req" and "MAIN.cache_hit", then
// those two stats are left out, while the remaining MAIN stats are
// read.
//
// Inclusion is evaluated before exclusion. For example, if MAIN.* is
// included but MAIN.client_req is excluded, then all of the MAIN
// stats except for client_req will be read.
func (stats *Stats) Include(nmGlob string) error {
	return stats.clude(nmGlob, false)
}

// Exclude filters out statistics whose names match the pattern in
// nmGlob, which may include wildcards. For example, the argument may
// be "MAIN.client_req" to remove only that stat, or "MAIN.*" to
// exclude all MAIN stats.
//
// See the documentation for Include for further notes about inclusion
// and exclusion of stats.
func (stats *Stats) Exclude(nmGlob string) error {
	return stats.clude(nmGlob, true)
}

func c2goLvl(clvl *C.struct_VSC_level_desc) Level {
	lvl := Level{}
	lvl.Label = C.GoString(clvl.label)
	lvl.ShortD9n = C.GoString(clvl.sdesc)
	lvl.LongD9n = C.GoString(clvl.ldesc)
	return lvl
}

func setLvls() []Level {
	var lvls []Level
	lastLvl := C.VSC_ChangeLevel(nil, 0)
	lvls = append(lvls, c2goLvl(lastLvl))
	for newLvl := C.VSC_ChangeLevel(lastLvl, 1); newLvl != lastLvl; {
		lvls = append(lvls, c2goLvl(newLvl))
		lastLvl = newLvl
		newLvl = C.VSC_ChangeLevel(newLvl, 1)
	}
	return lvls
}

//export description
func description(ptr unsafe.Pointer, key uint64) {
	if ptr == nil {
		return
	}
	data, ok := dataMap[key]
	if !ok {
		panic("unmapped stats data")
	}

	pt := (*C.struct_VSC_point)(ptr)
	name := C.GoString(pt.name)
	desc := D9n{}
	desc.ShortD9n = C.GoString(pt.sdesc)
	desc.LongD9n = C.GoString(pt.ldesc)
	desc.Semantics = Semantics(pt.semantics)
	desc.Format = Format(pt.format)
	label := C.GoString(pt.level.label)
	for _, lvl := range Levels {
		if lvl.Label == label {
			desc.Level = &lvl
			break
		}
	}
	data.d9ns[name] = desc
	data.names = append(data.names, name)
}

// Attach to an instance of Varnish -- varnishd with a running worker
// process.
//
// If name is the empty string, then attach to the default instance --
// varnishd invoked without the -n option. Otherwise, attach to the
// named instance, as given in the -n option.
func (stats *Stats) Attach(name string) error {
	if err := stats.initVSM(); err != nil {
		return err
	}
	if err := stats.vsm.Attach(name); err != nil {
		return err
	}
	if stats.vsm.VSM == nil {
		panic("VSM handle is nil")
	}

	if stats.d9ns != nil || stats.names != nil {
		panic("Stats data already mapped")
	}
	data := statsData{d9ns: make(map[string]D9n)}
	key := atomic.AddUint64(&mapCtr, 1)
	dataMap[key] = &data
	// Don't need the read lock here, because this is only called
	// for Attach.
	C.descriptions(stats.vsc, (*C.struct_vsm)(stats.vsm.VSM),
		C.uint64_t(key))

	stats.names = data.names
	stats.d9ns = data.d9ns
	delete(dataMap, key)
	return nil
}

// D9n (for "description") returns the Description for the stat named
// by name.
//
// Returns a non-nil error if the Stats instance is not attached, or
// if there is no stat with that name.
func (stats *Stats) D9n(name string) (D9n, error) {
	var errD9n = D9n{}

	if err := stats.checkNil(); err != nil {
		return errD9n, err
	}
	if stats.vsm == nil {
		return errD9n, errors.New("not attached to a Varnish instance")
	}
	if stats.d9ns == nil {
		panic("descriptions not initialized")
	}
	d9n, ok := stats.d9ns[name]
	if !ok {
		return errD9n, errors.New("no such stat: " + name)
	}
	return d9n, nil
}

// Names returns the names of the stats in use by the attached Varnish
// instance. The names in the returned slice are in the order in which
// stats are read via Read (corresponds to the order displayed by
// varnishstat).
//
// Returns a non-nil error if the Stats instance is not attached.
func (stats *Stats) Names() ([]string, error) {
	if err := stats.checkNil(); err != nil {
		return nil, err
	}
	if stats.vsm == nil {
		return nil, errors.New("not attached to a Varnish instance")
	}
	if stats.names == nil {
		panic("stats names not initialized")
	}
	return stats.names, nil
}

// Release frees native resources associated with this client. You
// should always call Release when you're done using a Stats client,
// otherwise there is risk of resource leakage.
//
// It is wise to make use of Go's defer mechanism:
//
//	vstats := stats.New()
//	defer vstats.Release()
//	// ... do stuff with vstats
func (stats *Stats) Release() {
	// silently ignore if nil or uninitialized
	if stats == nil || stats.vsc == nil {
		return
	}
	if stats.vsm == nil {
		C.free(unsafe.Pointer(stats.vsc))
		return
	}
	if stats.vsm.VSM == nil {
		panic("VSM handle is nil")
	}
	C.VSC_Destroy(&stats.vsc, (*C.struct_vsm)(stats.vsm.VSM))
	// ignoring nil check errors
	stats.vsm.Destroy()
}

//export point
func point(ptr unsafe.Pointer, key uint64) C.int {
	if ptr == nil {
		return 0
	}
	cbLock.RLock()
	rdhndlr, ok := cbMap[key]
	cbLock.RUnlock()
	if !ok {
		panic("unmapped read handler")
	}
	pt := (*C.struct_VSC_point)(ptr)
	if rdhndlr(C.GoString(pt.name), uint64(*pt.ptr)) {
		return 0
	}
	return 1
}

// ReadHandler is the type of a callback function that is passed to
// the Read method, and is invoked by Read as it iterates through the
// stats for the attached Varnish instance. For each stat, the handler
// is invoked with its name and current value.
//
// The handler returns true or false to signal whether the stats
// iteration should continue. As long as the handler returns true, the
// iteration continues until it has been invoked for all stats. If the
// handler returns false, the iteration is aborted and the Read method
// finishes.
//
// The string and the value passed as the paramters of the handler
// were allocated as local variables during the execution of
// Read. They go out of scope (and hence are eligible for garbage
// collection) when Read finishes execution.
type ReadHandler func(name string, val uint64) bool

// Read reads the stats from the attached Varnish instance, and
// invokes rdHndler with their current values, as documented for
// ReadHandler.
//
// Note that the native read operation is locked per Stats instance,
// since it is not safe for concurrency with the same internal
// handles.  To implement concurrent reads without lock contention,
// create distinct Stats objects that are separately attached to the
// Varnish instance.
//
// Returns an error if rdHndlr is nil, if the Stats object is not
// attached to a log source, or if the native library could not
// initialize the read.
//
// Example:
//
//	// set up a map for the stats values
//	var stats := make(map[string]uint64)
//
//	// this callback will be passed to the Read method
//	rdHndlr := func(name string, val uint64) bool {
//		stats[name] = val
//		return true
//	}
//
//	// assuming that stats is an initialized Stats object that is
//	// attached to a Varnish instance
//	err = vstats.Read(rdHndler)
//	if err != nil {
//		// error handling ...
//	}
//
//	// do stuff with the stats map ...
func (stats *Stats) Read(rdHndlr ReadHandler) error {
	if err := stats.checkNil(); err != nil {
		return err
	}
	if stats.vsm == nil {
		return errors.New("not attached to a Varnish instance")
	}
	if stats.vsm.VSM == nil {
		panic("VSM handle is nil")
	}
	if rdHndlr == nil {
		return errors.New("callback is nil")
	}

	key := atomic.AddUint64(&mapCtr, 1)
	cbLock.Lock()
	cbMap[key] = rdHndlr
	cbLock.Unlock()

	stats.rdLock.Lock()
	C.stats(stats.vsc, (*C.struct_vsm)(stats.vsm.VSM), C.uint64_t(key))
	stats.rdLock.Unlock()

	cbLock.Lock()
	delete(cbMap, key)
	cbLock.Unlock()

	return nil
}
