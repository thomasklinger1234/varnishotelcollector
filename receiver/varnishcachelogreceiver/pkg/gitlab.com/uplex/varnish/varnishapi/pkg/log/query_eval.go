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

import (
	"bytes"
	"slices"
	"unicode"
)

func bitTest(bits [32]byte, tag uint8) bool {
	return (bits[tag>>3] & (1 << (tag & 7))) != 0
}

func (expr qExpr) testVXID(tx Tx) bool {
	n := uint32(expr.rhs.intVal)
	switch expr.tok {
	case eq:
		return tx.VXID == n
	case neq:
		return tx.VXID != n
	case lt:
		return tx.VXID < n
	case gt:
		return tx.VXID > n
	case leq:
		return tx.VXID <= n
	case geq:
		return tx.VXID >= n
	}
	return false
}

func (expr qExpr) testRecord(rec Record) bool {
	// Defensive trim: VSLC_ptr.payload() (see vsl_int.go LOCAL PATCH) already
	// strips trailing non-graphic bytes so this is normally a no-op, but
	// callers that construct a Record directly (tests, future consumers)
	// still hit this path with raw payloads. Left in place, trailing NUL /
	// CR / LF would silently break every operator here: regex '$' would
	// not match, bytes.Equal would disagree on length, parseInt64 /
	// parseFloat64 would reject the trailing garbage.
	payload := bytes.TrimRightFunc(rec.Payload, func(r rune) bool {
		return !unicode.IsGraphic(r)
	})
	if len(expr.lhs.prefix) > 0 {
		l := len(expr.lhs.prefix)
		if l > len(payload) {
			return false
		}
		if !bytes.EqualFold(expr.lhs.prefix, payload[:l]) {
			return false
		}
		if payload[l] != byte(':') {
			return false
		}
		payload = payload[l+1:]
		payload = bytes.TrimLeft(payload, " \t\n\r")
	}
	if expr.lhs.field > 0 {
		min, max, ok := fieldNDelims(payload, expr.lhs.field-1)
		if !ok {
			return false
		}
		payload = payload[min:max]
	}

	if expr.rhs.rhsType == empty {
		return true
	}

	if expr.tok > numericBegin && expr.tok < numericEnd {
		if len(payload) == 0 {
			return false
		}
		// XXX byte slice-only versions of the numeric conversions
		switch expr.rhs.rhsType {
		case integer:
			val, ok := parseInt64(payload)
			if !ok {
				return false
			}
			n := expr.rhs.intVal
			switch expr.tok {
			case eq:
				return val == n
			case neq:
				return val != n
			case lt:
				return val < n
			case gt:
				return val > n
			case leq:
				return val <= n
			case geq:
				return val >= n
			}
		case float:
			val, ok := parseFloat64(payload)
			if !ok {
				return false
			}
			n := expr.rhs.floatVal
			switch expr.tok {
			case eq:
				return val == n
			case neq:
				return val != n
			case lt:
				return val < n
			case gt:
				return val > n
			case leq:
				return val <= n
			case geq:
				return val >= n
			}
		}
	}

	// XXX vsl_query.c has the CASELESS option
	switch expr.tok {
	case seq:
		return bytes.Equal(payload, expr.rhs.strVal)
	case sneq:
		return !bytes.Equal(payload, expr.rhs.strVal)
	case match:
		return expr.rhs.regex.Match(payload)
	case nomatch:
		return !expr.rhs.regex.Match(payload)
	}
	return false
}

func (expr qExpr) testTxGrp(txGrp []Tx) bool {
	if expr.lhs.vxid {
		return slices.ContainsFunc(txGrp, expr.testVXID)
	}

	for _, tx := range txGrp {
		if expr.lhs.level >= 0 {
			switch {
			case expr.lhs.lvlCmp < 0:
				if tx.Level > uint(expr.lhs.level) {
					continue
				}
			case expr.lhs.lvlCmp > 0:
				if tx.Level < uint(expr.lhs.level) {
					continue
				}
			default:
				if tx.Level != uint(expr.lhs.level) {
					continue
				}
			}
		}
		for _, rec := range tx.Records {
			if !bitTest(expr.lhs.tags, uint8(rec.Tag)) {
				continue
			}
			if expr.testRecord(rec) {
				return true
			}
		}
	}
	return false
}

func (expr qExpr) eval(txGrp []Tx) bool {
	switch expr.tok {
	case or:
		if expr.a.eval(txGrp) {
			return true
		}
		return expr.b.eval(txGrp)
	case and:
		if !expr.a.eval(txGrp) {
			return false
		}
		return expr.b.eval(txGrp)
	case not:
		return !expr.a.eval(txGrp)
	default:
		return expr.testTxGrp(txGrp)
	}
}
