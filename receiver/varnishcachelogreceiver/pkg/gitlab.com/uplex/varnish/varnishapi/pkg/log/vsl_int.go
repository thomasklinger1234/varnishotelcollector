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
#include <vapi/vsl_int.h>

// Helper to get tag from VSL record pointer
static inline int vsl_tag(const uint32_t *ptr) {
	return (VSL_TAG(ptr));
}

static inline int vsl_vxid(const uint32_t *ptr) {
	return VSL_ID(ptr);
}

static inline int vsl_len(const uint32_t *ptr) {
	return VSL_LEN(ptr);
}

static inline const char *vsl_data(const uint32_t *ptr) {
	return VSL_CDATA(ptr);
}

static inline int vsl_client(const uint32_t *ptr) {
	return VSL_CLIENT(ptr);
}

static inline int vsl_backend(const uint32_t *ptr) {
	return VSL_BACKEND(ptr);
}
*/
import "C"

import "unsafe"

// VSLC_ptr wraps *C.struct_VSLC_ptr to allow method definitions
type VSLC_ptr struct {
	ptr *C.struct_VSLC_ptr
}

func (rec *VSLC_ptr) tag() Tag {
	return Tag(C.vsl_tag(rec.ptr.ptr))
}

func (rec *VSLC_ptr) vxid() uint32 {
	return uint32(C.vsl_vxid(rec.ptr.ptr))
}

func (rec *VSLC_ptr) length() uint32 {
	return uint32(C.vsl_len(rec.ptr.ptr))
}

// payload copies the C-side VSL record payload into a fresh Go slice.
//
// LOCAL PATCH (varnishotelcollector): the upstream v1.0.0 implementation
// used a fake fixed-size array cast (*[4096]byte)(unsafe.Pointer(...)),
// which caused `slice bounds out of range [:N] with length 4096` panics
// whenever a VSL record payload exceeded 4096 bytes. Varnish itself
// allows records up to 65535 bytes (VSL_LENMASK = 0xffff in
// <vapi/vsl_int.h>, and the vsl_reclen runtime parameter caps at 65535).
//
// C.GoBytes allocates a Go slice of exactly `length` bytes and copies
// the C data into it, so we honour whatever length the record header
// declares without any hardcoded ceiling.
func (rec *VSLC_ptr) payload() []byte {
	length := rec.length()
	if length == 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(C.vsl_data(rec.ptr.ptr)), C.int(length))
}

func (rec *VSLC_ptr) client() bool {
	return C.vsl_client(rec.ptr.ptr) != 0
}

func (rec *VSLC_ptr) backend() bool {
	return C.vsl_backend(rec.ptr.ptr) != 0
}
