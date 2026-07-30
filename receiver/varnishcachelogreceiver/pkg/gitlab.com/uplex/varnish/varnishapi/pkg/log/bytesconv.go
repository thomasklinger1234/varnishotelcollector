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

import "math"

const (
	cutoffU32    = ^uint32(0) / 10
	restU32      = byte(^uint32(0)-cutoffU32*10) + byte('0')
	maxSigned64  = int64(^uint64(1 << 63))
	cutoffDec64  = int64(maxSigned64 / 10)
	cutoffOct64  = int64(1 << 60)
	cutoffHex64  = int64(1 << 59)
	restPosDec64 = byte(maxSigned64-cutoffDec64*10) + byte('0')
	restNegDec64 = byte(restPosDec64 + 1)
	ill          = uint8(255)
)

var (
	wsTable = wsTbl()
	hxTbl   = [55]uint8{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
		ill, ill, ill, ill, ill, ill, ill, 10, 11, 12,
		13, 14, 15, ill, ill, ill, ill, ill, ill, ill,
		ill, ill, ill, ill, ill, ill, ill, ill, ill, ill,
		ill, ill, ill, ill, ill, ill, ill, ill, ill, 10,
		11, 12, 13, 14, 15,
	}
)

func wsTbl() [256]bool {
	var tbl [256]bool
	tbl[int(' ')] = true
	tbl[int('\t')] = true
	tbl[int('\n')] = true
	tbl[int('\r')] = true
	return tbl
}

// Parse the byte slice, containing ASCII decimal digits, as an
// uint32.  The boolean return is false if the parse fails.
func atoUint32(bytes []byte) (uint32, bool) {
	if len(bytes) == 0 {
		return 0, false
	}
	val := uint32(0)
	for _, b := range bytes {
		if val >= cutoffU32 {
			if val > cutoffU32 {
				return 0, false
			}
			if b > restU32 {
				return 0, false
			}
		}
		if b < byte('0') || b > byte('9') {
			return 0, false
		}
		val = val*10 + uint32(b-byte('0'))
	}
	return val, true
}

func parseOct(bytes []byte, pos bool) (int64, bool) {
	val := int64(0)
	for _, b := range bytes {
		if val >= cutoffOct64 {
			if pos || val != cutoffOct64 || b != byte('0') {
				return 0, false
			}
		}
		if b < byte('0') || b > byte('7') {
			if wsTable[b] {
				break
			}
			return 0, false
		}
		val <<= 3
		val |= int64(b - byte('0'))
	}
	if pos {
		return val, true
	}
	return 0 - val, true
}

func parseHex(bytes []byte, pos bool) (int64, bool) {
	val := int64(0)
	for _, b := range bytes {
		if val >= cutoffHex64 {
			if pos || val != cutoffHex64 || b != byte('0') {
				return 0, false
			}
		}
		if b < byte('0') || b > byte('f') || hxTbl[b-byte('0')] == ill {
			if wsTable[b] {
				break
			}
			return 0, false
		}
		val <<= 4
		val |= int64(hxTbl[b-byte('0')])
	}
	if pos {
		return val, true
	}
	return 0 - val, true
}

func parseDec(bytes []byte, pos bool) (int64, bool) {
	rest := restPosDec64
	if !pos {
		rest = restNegDec64
	}
	val := int64(0)
	for _, b := range bytes {
		if val >= cutoffDec64 {
			if val > cutoffDec64 {
				return 0, false
			}
			if b > rest {
				return 0, false
			}
		}
		if b < byte('0') || b > byte('9') {
			if wsTable[b] {
				break
			}
			return 0, false
		}
		val = val*10 + int64(b-byte('0'))
	}
	if pos {
		return val, true
	}
	return 0 - val, true
}

// ParseInt(b, 0, 64), or strtoll(b, &p, 0), for byte slices.
// Only whitespace after the digits is permitted.
func parseInt64(bytes []byte) (int64, bool) {
	l := len(bytes)
	if l == 0 {
		return 0, false
	}
	pos := true
	i := 0
	for ; i < l && wsTable[bytes[i]]; i++ {
	}
	if i == l {
		return 0, false
	}
	if bytes[i] == byte('+') || bytes[i] == byte('-') {
		if bytes[i] == byte('-') {
			pos = false
		}
		i++
		if i == l {
			return 0, false
		}
	}
	if bytes[i] == byte('0') {
		if i == l {
			return 0, true
		}
		if i+1 < l && bytes[i+1] == byte('x') {
			return parseHex(bytes[i+2:], pos)
		}
		return parseOct(bytes[i+1:], pos)
	}
	return parseDec(bytes[i:], pos)
}

func parseE(bytes []byte, i int) (float64, int, bool) {
	ee, es := float64(0.), float64(1.)
	i++
	if bytes[i] == byte('+') || bytes[i] == byte('-') {
		if len(bytes) == 1 {
			return 0., 0, false
		}
		if bytes[i+1] < byte('0') || bytes[i+1] > byte('9') {
			return 0., 0, false
		}
		if bytes[i] == byte('-') {
			es = -1.
		}
		i++
	}
	for ; i < len(bytes); i++ {
		b := bytes[i]
		if b < byte('0') || b > byte('9') {
			break
		}
		ee = ee*10. + float64(b-byte('0'))
	}
	return ee * es, i, true
}

// Parse a double precision float from a byte slice. Adapted from Varnish
// VNUMpfx() in vnum.c.
//
// Handles scientific notation, but not special values like NaN, Inf or
// negative zero (-0. is intepreted as just 0.).
//
// Leading whitespace is ignored. Whitespace immediately after the digits
// is permitted (and then anything else is ignored).
//
// Results may not be accurate up to the limits of DBL_MIN and DBL_MAX
// (or I am unable to test for that). It is accurate up to
// +-(1 << DBL_MANT_DIG), and certainly for typical Varnish log usage
// (mainly, to parse Timestamp entries).
func parseFloat64(bytes []byte) (float64, bool) {
	m, ne := float64(0.), float64(0.)
	ms, e := float64(1.), float64(1.)
	l := len(bytes)
	i := 0

	for ; i < l && wsTable[bytes[i]]; i++ {
	}
	if i == l {
		return 0., false
	}
	if bytes[i] == byte('+') || bytes[i] == byte('-') {
		if bytes[i] == byte('-') {
			ms = -1.
		}
		i++
		if i == l {
			return 0., false
		}
	}
	for ; i < l; i++ {
		b := bytes[i]
		if b >= byte('0') && b <= byte('9') {
			m = m*10. + float64(b-byte('0'))
			e = ne
			if e != 0.0 {
				ne = e - 1.
			}
			continue
		}
		if b == byte('.') && ne == 0. {
			ne = -1.0
			continue
		}
		break
	}
	if e > 0. {
		return 0., false
	}
	if i < l && (bytes[i] == byte('e') || bytes[i] == byte('E')) {
		exp, idx, ok := parseE(bytes, i)
		if !ok {
			return 0., false
		}
		e += exp
		i = idx
	}
	if i < l && !wsTable[bytes[i]] {
		return 0., false
	}
	return ms * m * math.Pow(10., e), true
}

// Return the byte slice delimiters of the Nth whitespace-separated
// field in the given byte slice, counting fields from 0 and ignoring
// leading and trailing whitespace:
//
//	min, max, ok := fieldNDelims(bytes, 1)
//	if !ok {
//		// ... error handling
//	}
//	fld1 := bytes[min:max]
//	// fld1 now contains the region of field 1.
//
// Whitespace as defined for ASCII: ' ', '\t', '\n', '\r'
//
// The boolean return value is false if field < 0 or greater than the
// number of fields in the slice, or if the length of the slice is
// 0. Note that the nil slice has length 0.
//
// If there is no whitespace separating bytes in the slice, then 0 is
// the only field, encompassing the entire whitespace-trimmed slice.
func fieldNDelims(bytes []byte, field int) (int, int, bool) {
	var b, e, i int
	if field < 0 {
		return 0, 0, false
	}
	l := len(bytes)
	for b, e, i = 0, 0, 0; e < l && i <= field; i++ {
		b = e
		for b < l && wsTable[bytes[b]] {
			b++
		}
		e = b
		for e < l && !wsTable[bytes[e]] {
			e++
		}
	}
	if b == l || i <= field {
		return 0, 0, false
	}
	return b, e, true
}
