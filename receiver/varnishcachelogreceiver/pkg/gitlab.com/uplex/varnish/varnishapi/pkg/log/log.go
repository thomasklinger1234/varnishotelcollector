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

// Package log implements a client for the Varnish native logging
// interface.
package log

/*
#cgo pkg-config: varnishapi
#include <stdio.h>
#include <vapi/vsl.h>

unsigned
vsl_opt_batch()
{
        return VSL_COPT_BATCH;
}

unsigned
vsl_opt_tail()
{
        return VSL_COPT_TAIL;
}

unsigned
vsl_opt_tailstop()
{
        return VSL_COPT_TAILSTOP;
}
*/
import "C"

import (
	"gitlab.com/uplex/varnish/varnishapi/pkg/vsm"

	"errors"
	"time"
)

// A Log instance is the foundation for log access via the native
// interface. It can be used to attach to the log of a live instance
// of Varnish, create a Cursor object, and set include/exclude filters
// for the records to be read from the log.
//
// XXX include/exclude filters not yet implemented
type Log struct {
	vsl     *C.struct_VSL_data
	vsm     *vsm.VSM
	vsmopts C.unsigned
}

// New returns a new and initialized instance of Log. Log instances
// should only be created through this method, since it performs
// initializations for the native library that are required for the
// other methods.
//
// New may return nil in rare cases, usually in an out of memory
// condition.
func New() *Log {
	log := new(Log)
	log.vsl = C.VSL_New()
	if log.vsl == nil {
		return nil
	}
	log.vsm = nil
	log.vsmopts = C.vsl_opt_tail()
	return log
}

func (log *Log) checkNil() error {
	if log == nil {
		return errors.New("Log object is nil")
	}
	if log.vsl == nil {
		return errors.New("Log object was not initialized via New()")
	}
	return nil
}

// Release frees native resources associated with this client. You
// should always call Release when you're done using a Log client,
// otherwise there is risk of resource leakage.
//
// It is wise to make use of Go's defer mechanism:
//
//	vlog := log.New()
//	defer vlog.Release()
//	// ... do stuff with vlog
func (log *Log) Release() {
	// silently ignore if nil or uninitialized
	if log == nil || log.vsl == nil {
		return
	}
	if log.vsm != nil {
		// ignoring nil check errors
		_ = log.vsm.Destroy()
	}
	C.VSL_Delete(log.vsl)
}

// Error returns the most recent error message from the native
// interface.
func (log *Log) Error() string {
	if err := log.checkNil(); err != nil {
		return err.Error()
	}
	return C.GoString(C.VSL_Error(log.vsl))
}

func (log *Log) initVSM() error {
	if err := log.checkNil(); err != nil {
		return err
	}
	if log.vsm == nil {
		log.vsm = vsm.New()
		if log.vsm == nil {
			return errors.New("Cannot initialize for attachment " +
				"to shared memory")
		}
	}
	return nil
}

// Timeout sets the timeout for attaching to a Varnish instance.  The
// timeout is truncated to seconds. If the truncated timeout >= 0s,
// then wait that many seconds to attach. If the tmo < 0s, wait
// indefinitely. By default, the Varnish default timeout holds (5s in
// recent Varnish versions).
func (log *Log) Timeout(tmo time.Duration) error {
	if err := log.initVSM(); err != nil {
		return err
	}
	return log.vsm.Timeout(tmo)
}

// Attach to an instance of Varnish -- varnishd with a running worker
// process.
//
// If name is the empty string, then attach to the default instance --
// varnishd invoked without the -n option. Otherwise, attach to the
// named instance, as given in the -n option.
func (log *Log) Attach(name string) error {
	if err := log.initVSM(); err != nil {
		return err
	}
	if err := log.vsm.Attach(name); err != nil {
		return err
	}
	if log.vsm.VSM == nil {
		panic("VSM handle is nil")
	}
	return nil
}
