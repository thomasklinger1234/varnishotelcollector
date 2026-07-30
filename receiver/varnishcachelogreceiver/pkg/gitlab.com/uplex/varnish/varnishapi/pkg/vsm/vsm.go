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

// Package vsm implements attachment to Varnish shared memory, to read
// logs, statistics and other information.
package vsm

/*
#cgo pkg-config: varnishapi
#include <stdint.h>
#include <stdlib.h>
#include <vapi/vsm.h>

struct status {
	unsigned mgt_running;
	unsigned mgt_changed;
	unsigned mgt_restarted;
	unsigned wrk_running;
	unsigned wrk_changed;
	unsigned wrk_restarted;
};

void
status(struct vsm *vsm, struct status *status)
{
	unsigned bits = VSM_Status(vsm);
	status->mgt_running = bits & VSM_MGT_RUNNING;
	status->mgt_changed = bits & VSM_MGT_CHANGED;
	status->mgt_restarted = bits & VSM_MGT_RESTARTED;
	status->wrk_running = bits & VSM_WRK_RUNNING;
	status->wrk_changed = bits & VSM_WRK_CHANGED;
	status->wrk_restarted = bits & VSM_WRK_RESTARTED;
}
*/
import "C"

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// VSM provides methods for attaching to Varnish shared memory.
type VSM struct {
	// VSM is an native handle for use by other packages.  Client
	// code should never use it.
	VSM *C.struct_vsm
}

// New returns a new and initialied instance of VSM. Instances should
// only be created through this function, since it performs
// initializations for the native library that are required for the
// object methods.
//
// New may return nil in rare cases, usually in an out of memory
// condition.
func New() *VSM {
	vsmp := C.VSM_New()
	if vsmp == nil {
		return nil
	}
	return &VSM{vsmp}
}

func (v *VSM) checkNil() error {
	if v == nil {
		return errors.New("VSM object is nil")
	}
	if v.VSM == nil {
		return errors.New("VSM object was uninitialized via New()")
	}
	return nil
}

// Destroy releases native resources associated with this object. You
// should always call Destroy when you're done using with a VSM
// object, otherwise there is risk of resource leakage.
func (v *VSM) Destroy() error {
	if err := v.checkNil(); err != nil {
		return err
	}
	C.VSM_Destroy(&v.VSM)
	return nil
}

// Error returns the most recent error message from the native
// library.
func (v *VSM) Error() string {
	if err := v.checkNil(); err != nil {
		return err.Error()
	}
	return C.GoString(C.VSM_Error(v.VSM))
}

// Timeout sets the timeout for attaching to a Varnish instance. If
// the Duration, truncated to seconds, is >= 0s, then wait that many
// seconds to attach. If the Duration < 0s, wait indefinitely. By
// default, the Varnish default timeout holds (5s in recent Varnish
// versions).
func (v *VSM) Timeout(tmo time.Duration) error {
	if err := v.checkNil(); err != nil {
		return err
	}
	arg := "off"
	secs := tmo.Truncate(time.Second)
	if secs >= 0 {
		arg = strconv.FormatUint(uint64(secs), 10)
	}
	C.VSM_ResetError(v.VSM)
	if C.VSM_Arg(v.VSM, 't', C.CString(arg)) != 1 {
		return v
	}
	return nil
}

// Attach to an instance of Varnish -- varnishd with a running worker
// process.
//
// If name is the empty string, then attach to the default instance --
// varnishd invoked without the -n option. Otherwise, attach to the
// named instance, as given in the -n option.
func (v *VSM) Attach(name string) error {
	if err := v.checkNil(); err != nil {
		return err
	}
	if name != "" {
		C.VSM_ResetError(v.VSM)
		if C.VSM_Arg(v.VSM, 'n', C.CString(name)) != 1 {
			return v
		}
	}
	C.VSM_ResetError(v.VSM)
	if C.VSM_Attach(v.VSM, C.int(-1)) != 0 {
		return v
	}
	return nil
}

// A Status contains information about the state of the Varnish
// management and child processes since the last time Status() was
// called, or since the VSM client attached.
//
// For the two processes: whether the process is running, whether it
// was restarted, or whether the internal mappings have changed (for
// example, when statistics have been added or removed at runtime).
type Status struct {
	MgtRunning   bool
	MgtChanged   bool
	MgtRestarted bool
	WrkRunning   bool
	WrkChanged   bool
	WrkRestarted bool
}

// Status returns a Status object describing the state of the Varnish
// management and worker processes, and whether internal mappings have
// changed since the last invocation of Status(), or (if it was never
// called) since the instance was attached.
func (v *VSM) Status() (Status, error) {
	errStatus := Status{}
	if err := v.checkNil(); err != nil {
		return errStatus, err
	}
	var cstatus C.struct_status
	C.VSM_ResetError(v.VSM)
	C.status(v.VSM, &cstatus)
	return Status{
		MgtRunning:   cstatus.mgt_running != 0,
		MgtChanged:   cstatus.mgt_changed != 0,
		MgtRestarted: cstatus.mgt_restarted != 0,
		WrkRunning:   cstatus.wrk_running != 0,
		WrkChanged:   cstatus.wrk_changed != 0,
		WrkRestarted: cstatus.wrk_restarted != 0,
	}, nil
}

// Get returns the string from internal mappings identified by class
// and ident. Return non-nil error if no such string was found.
func (v *VSM) Get(class string, ident string) (string, error) {
	if err := v.checkNil(); err != nil {
		return "", err
	}
	cdup := C.VSM_Dup(v.VSM, C.CString(class), C.CString(ident))
	if cdup == nil {
		return "", errors.New("not found: " + class + ", " + ident)
	}
	dup := C.GoString(cdup)
	C.free(unsafe.Pointer(cdup))
	return dup, nil
}

// GetMgmtAddr is a convenience wrapper for Get() that returns the
// network address at which Varnish is listening for management
// commands, according to the protocol documented in
// varnish-cli(7). The address may have been set with the Varnish -T
// option, or Varnish may have generated the address itself (if ot -T
// option was set).
//
// Clients such as the admin package or varnishadm(1) can connect to
// this address and issue commands to the Varnish management process.
//
// The address is a string of the form "address:port", so it is
// suitable for use with the Dial() method of the Admin object.
func (v *VSM) GetMgmtAddr() (string, error) {
	targ, err := v.Get("Arg", "-T")
	if err != nil {
		return "", err
	}
	targ = strings.TrimSpace(targ)
	targ = strings.Replace(targ, " ", ":", 1)
	return targ, nil
}

// GetSecretPath is a convenience wrapper for Get() that returns the
// path of the file that Varnish uses for authenticating connections
// to the management address. The authentication protocol is
// documented in varnish-cli(7).
func (v *VSM) GetSecretPath() (string, error) {
	sarg, err := v.Get("Arg", "-S")
	if err != nil {
		return "", err
	}
	sarg = strings.TrimSpace(sarg)
	return sarg, nil
}
