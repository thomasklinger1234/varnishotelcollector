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

/*
#cgo pkg-config: varnishapi
#include <stdio.h>
#include <vapi/vsl.h>
*/
import "C"

import "errors"

// A Cursor is used to advance through the records in a Varnish
// log. It can be created from an attached instance of Log with
// NewCursor(), to read from a live Varnish instance, or with
// Log.NewCursorFile(), to read from a binary log file (as produced by
// varnishlog -w).
//
// A Cursor can in turn be used to create a Query object for reading
// aggregated log transactions. Or you can use Cursor's Next() method
// to read disaggregated Records directly from the log (equivalent to
// reading raw transactions via a Query). The results of intermingled
// reads via Next() and via a Query created from the same Cursor are
// undefined.
//
// Distinct Cursors may be used to read a log concurrently. Concurrent
// reads with the same Cursor are not safe.
//
// Because they are low-level operations used frequently in hot code,
// the Cursor methods have no error checking. So it is vital to
// observe these two rules:
//
// The Cursor methods will panic if applied to a nil Cursor, or to a
// Cursor that has not been initialized through one of the New*
// functions.
//
// Call Next() before using one of the methods that read data from the
// log record to which a Cursor is concurrently pointing (Match(),
// Tag(), VXID(), Payload(), Client(), Backend() or Record()). The
// results of calling one of these methods before Next() are
// undefined, and may lead to panic.
type Cursor struct {
	cursor *C.struct_VSL_cursor
	log    *Log
}

// NewCursor creates a Cursor to read from a live Varnish log. Fails
// if the Log object is not attached to a log.
func (log *Log) NewCursor() (*Cursor, error) {
	if err := log.checkNil(); err != nil {
		return nil, err
	}
	if log.vsm == nil {
		return nil, errors.New("Not attached to a Varnish log")
	}
	if log.vsm.VSM == nil {
		panic("VSM handle is nil")
	}
	C.VSL_ResetError(log.vsl)
	cursor := C.VSL_CursorVSM(log.vsl, (*C.struct_vsm)(log.vsm.VSM),
		log.vsmopts)
	if cursor == nil {
		return nil, log
	}
	return &Cursor{cursor: cursor, log: log}, nil
}

// NewCursorFile creates a Cursor to read from a binary log file,
// created by varnishlog -w.
//
// Fails if the Log object is already attached to a live Varnish log.
func (log *Log) NewCursorFile(path string) (*Cursor, error) {
	if err := log.checkNil(); err != nil {
		return nil, err
	}
	if log.vsm != nil {
		return nil, errors.New("Already attached to a Varnish log")
	}
	C.VSL_ResetError(log.vsl)
	cursor := C.VSL_CursorFile(log.vsl, C.CString(path), 0)
	if cursor == nil {
		return nil, log
	}
	return &Cursor{cursor: cursor, log: log}, nil
}

// Delete releases native resources associated with the Cursor. You
// should always call Delete when you're done with a Cursor, otherwise
// there is a a risk of resource leakage. It is wise to schedule
// invocation with defer after successfully creating a Cursor with one
// of the New*() functions.
func (c *Cursor) Delete() {
	if c == nil || c.cursor == nil {
		// return silently in the nil case
		return
	}
	C.VSL_DeleteCursor(c.cursor)
	c.cursor = nil
}

// Next advances the Cursor to the next Record in the log. It returns
// a Status to indicate an error condition, the end of the log (EOF or
// EOL), or that there are more records in the log.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Next() Status {
	return Status(C.VSL_Next(c.cursor))
}

// Match returns true if the record at which the Cursor is currently
// pointing should be included, according to the include/exclude
// filters set for the Log object with which the Cursor was created.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Match() bool {
	return C.VSL_Match(c.log.vsl, c.cursor) == 1
}

// Tag returns the Tag of the record at which the Cursor is currently
// pointing.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Tag() Tag {
	return (&VSLC_ptr{ptr: &c.cursor.rec}).tag()
}

// VXID returns the VXID of the record at which the Cursor is
// currently pointing.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) VXID() uint32 {
	return (&VSLC_ptr{ptr: &c.cursor.rec}).vxid()
}

// Payload returns the payload (message content) of the record at
// which the Cursor is currently pointing.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Payload() []byte {
	return (&VSLC_ptr{ptr: &c.cursor.rec}).payload()
}

// Client returns true if the record at which the Cursor is currently
// pointing belongs to a client transaction.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Client() bool {
	return (&VSLC_ptr{ptr: &c.cursor.rec}).client()
}

// Backend returns true if the record at which the Cursor is currently
// pointing belongs to a backend transaction.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Backend() bool {
	return (&VSLC_ptr{ptr: &c.cursor.rec}).backend()
}

// Record returns structured data for the record at which the Cursor
// is currently pointing.
//
// Panics if Cursor is nil, or if was not initialized with one of the
// New* functions.
func (c *Cursor) Record() Record {
	rec := Record{
		Tag:     (&VSLC_ptr{ptr: &c.cursor.rec}).tag(),
		VXID:    uint((&VSLC_ptr{ptr: &c.cursor.rec}).vxid()),
		Payload: Payload((&VSLC_ptr{ptr: &c.cursor.rec}).payload()),
	}
	switch {
	case (&VSLC_ptr{ptr: &c.cursor.rec}).backend():
		rec.Type = Backend
	case (&VSLC_ptr{ptr: &c.cursor.rec}).client():
		rec.Type = Client
	default:
		rec.Type = None
	}
	return rec
}
