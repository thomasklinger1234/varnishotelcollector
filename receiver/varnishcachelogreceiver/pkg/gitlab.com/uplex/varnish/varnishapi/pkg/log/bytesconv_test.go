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
	"strconv"
	"testing"
)

type atoUint32Exp struct {
	val uint32
	ok  bool
}

func TestAtoUint32(t *testing.T) {
	expMap := map[string]atoUint32Exp{
		"":           {val: 0, ok: false},
		"0":          {val: 0, ok: true},
		"1":          {val: 1, ok: true},
		"9":          {val: 9, ok: true},
		"10":         {val: 10, ok: true},
		"101":        {val: 101, ok: true},
		"1009":       {val: 1009, ok: true},
		"foo":        {val: 0, ok: false},
		"4294967290": {val: 4294967290, ok: true},
		"4294967291": {val: 4294967291, ok: true},
		"4294967292": {val: 4294967292, ok: true},
		"4294967293": {val: 4294967293, ok: true},
		"4294967294": {val: 4294967294, ok: true},
		"4294967295": {val: 4294967295, ok: true},
		"4294967296": {val: 0, ok: false},
		"4294967297": {val: 0, ok: false},
		"4294967298": {val: 0, ok: false},
		"4294967299": {val: 0, ok: false},
	}

	for str, exp := range expMap {
		val, ok := atoUint32([]byte(str))
		if val != exp.val || ok != exp.ok {
			t.Errorf("atoUint32(%s) want=%v got=%v", str, exp,
				atoUint32Exp{val: val, ok: ok})
		}
	}
}

func BenchmarkAtoUint32(b *testing.B) {
	bytes := []byte("4294967295")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := atoUint32(bytes)
		if !ok {
			b.Fatal("atoUint32() failed to parse")
			return
		}
	}
}

func BenchmarkStrconvAtoi(b *testing.B) {
	bytes := []byte("4294967295")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := strconv.Atoi(string(bytes))
		if err != nil {
			b.Fatal("strconv.Atoi() failed to parse")
			return
		}
	}
}

var delimVec = map[string][]struct {
	fld int
	ok  bool
	exp string
}{
	"foo bar baz quux": {
		{0, true, "foo"},
		{1, true, "bar"},
		{2, true, "baz"},
		{3, true, "quux"},
		{4, false, ""},
		{5, false, ""},
	},
	"  foo \t \n  bar\r  \t   baz    quux\n\r\n\n": {
		{0, true, "foo"},
		{1, true, "bar"},
		{2, true, "baz"},
		{3, true, "quux"},
		{4, false, ""},
		{5, false, ""},
	},
	"foobarbazquux": {
		{0, true, "foobarbazquux"},
		{1, false, ""},
		{2, false, ""},
	},
	" \t\n\r foobarbazquux\r\n\n\n": {
		{0, true, "foobarbazquux"},
		{1, false, ""},
		{2, false, ""},
	},
	"foobarbazquux     ": {
		{0, true, "foobarbazquux"},
		{1, false, ""},
		{2, false, ""},
	},
	"     foobarbazquux": {
		{0, true, "foobarbazquux"},
		{1, false, ""},
		{2, false, ""},
	},
	"": {
		{0, false, ""},
		{1, false, ""},
		{2, false, ""},
	},
}

func TestFieldNDelims(t *testing.T) {
	s := []byte("foo bar baz quux")
	_, _, ok := fieldNDelims(s, 4)
	if ok {
		t.Error("fieldNDelims() did not fail for N out of range")
	}
	_, _, ok = fieldNDelims(s, -1)
	if ok {
		t.Error("fieldNDelims() did not fail for N < 0")
	}
	_, _, ok = fieldNDelims(nil, 0)
	if ok {
		t.Error("fieldNDelims() did not fail the nil slice")
	}

	for str, expVec := range delimVec {
		sl := []byte(str)
		for _, exp := range expVec {
			min, max, ok := fieldNDelims(sl, exp.fld)
			if ok != exp.ok {
				t.Errorf("fieldNdelims(%s, %d) ok want=%v "+
					"got=%v", str, exp.fld, exp.ok, ok)
			}
			if exp.ok {
				f := string(sl[min:max])
				if f != exp.exp {
					t.Errorf("fieldNDelims(%s, %d) want=%v"+
						" got=%v", str, exp.fld,
						exp.exp, f)
				}
			}
		}
	}
}

func BenchmarkFieldNDelims(b *testing.B) {
	bytes := []byte("foo bar baz quux")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, ok := fieldNDelims(bytes, 3)
		if !ok {
			b.Fatal("fieldNDelims() failed to parse")
			return
		}
	}
}

func BenchmarkBytesFields(b *testing.B) {
	s := []byte("foo bar baz quux")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bytes.Fields(s)
	}
}

func TestParseInt64(t *testing.T) {
	expMap := map[string]struct {
		val int64
		ok  bool
	}{
		"":            {val: 0, ok: false},
		"0":           {val: 0, ok: true},
		"1":           {val: 1, ok: true},
		"9":           {val: 9, ok: true},
		"10":          {val: 10, ok: true},
		"101":         {val: 101, ok: true},
		"1009":        {val: 1009, ok: true},
		"foo":         {val: 0, ok: false},
		" foo ":       {val: 0, ok: false},
		"foo  ":       {val: 0, ok: false},
		"  foo":       {val: 0, ok: false},
		"4294967290":  {val: 4294967290, ok: true},
		"4294967291":  {val: 4294967291, ok: true},
		"4294967292":  {val: 4294967292, ok: true},
		"4294967293":  {val: 4294967293, ok: true},
		"4294967294":  {val: 4294967294, ok: true},
		"4294967295":  {val: 4294967295, ok: true},
		"4294967296":  {val: 4294967296, ok: true},
		"4294967297":  {val: 4294967297, ok: true},
		"4294967298":  {val: 4294967298, ok: true},
		"4294967299":  {val: 4294967299, ok: true},
		"+0":          {val: 0, ok: true},
		"+1":          {val: 1, ok: true},
		"+9":          {val: 9, ok: true},
		"+10":         {val: 10, ok: true},
		"+101":        {val: 101, ok: true},
		"+1009":       {val: 1009, ok: true},
		"+4294967299": {val: 4294967299, ok: true},
		"-0":          {val: 0, ok: true},
		"-1":          {val: -1, ok: true},
		"-9":          {val: -9, ok: true},
		"-10":         {val: -10, ok: true},
		"-101":        {val: -101, ok: true},
		"-1009":       {val: -1009, ok: true},
		"-4294967299": {val: -4294967299, ok: true},
		" +1":         {val: 1, ok: true},
		" -1":         {val: -1, ok: true},

		"00":       {val: 0, ok: true},
		"01":       {val: 1, ok: true},
		"02":       {val: 2, ok: true},
		"03":       {val: 3, ok: true},
		"04":       {val: 4, ok: true},
		"05":       {val: 5, ok: true},
		"06":       {val: 6, ok: true},
		"07":       {val: 7, ok: true},
		"08":       {val: 0, ok: false},
		"09":       {val: 0, ok: false},
		"010":      {val: 8, ok: true},
		"011":      {val: 9, ok: true},
		"012":      {val: 10, ok: true},
		"013":      {val: 11, ok: true},
		"014":      {val: 12, ok: true},
		"015":      {val: 13, ok: true},
		"016":      {val: 14, ok: true},
		"017":      {val: 15, ok: true},
		"0100":     {val: 64, ok: true},
		"0177":     {val: 127, ok: true},
		"01000":    {val: 512, ok: true},
		"01777":    {val: 1023, ok: true},
		"0187":     {val: 0, ok: false},
		"+00":      {val: 0, ok: true},
		"+01":      {val: 1, ok: true},
		"+02":      {val: 2, ok: true},
		"+03":      {val: 3, ok: true},
		"+04":      {val: 4, ok: true},
		"+05":      {val: 5, ok: true},
		"+06":      {val: 6, ok: true},
		"+07":      {val: 7, ok: true},
		"+08":      {val: 0, ok: false},
		"+09":      {val: 0, ok: false},
		"+010":     {val: 8, ok: true},
		"+011":     {val: 9, ok: true},
		"+012":     {val: 10, ok: true},
		"+013":     {val: 11, ok: true},
		"+014":     {val: 12, ok: true},
		"+015":     {val: 13, ok: true},
		"+016":     {val: 14, ok: true},
		"+017":     {val: 15, ok: true},
		"+0100":    {val: 64, ok: true},
		"+0177":    {val: 127, ok: true},
		"+01000":   {val: 512, ok: true},
		"+01777":   {val: 1023, ok: true},
		"-00":      {val: 0, ok: true},
		"-01":      {val: -1, ok: true},
		"-02":      {val: -2, ok: true},
		"-03":      {val: -3, ok: true},
		"-04":      {val: -4, ok: true},
		"-05":      {val: -5, ok: true},
		"-06":      {val: -6, ok: true},
		"-07":      {val: -7, ok: true},
		"-08":      {val: 0, ok: false},
		"-09":      {val: 0, ok: false},
		"-010":     {val: -8, ok: true},
		"-011":     {val: -9, ok: true},
		"-012":     {val: -10, ok: true},
		"-013":     {val: -11, ok: true},
		"-014":     {val: -12, ok: true},
		"-015":     {val: -13, ok: true},
		"-016":     {val: -14, ok: true},
		"-017":     {val: -15, ok: true},
		"-0100":    {val: -64, ok: true},
		"-0177":    {val: -127, ok: true},
		"-01000":   {val: -512, ok: true},
		"-01777":   {val: -1023, ok: true},
		" 01777 ":  {val: 1023, ok: true},
		" +01777 ": {val: 1023, ok: true},
		" -01777 ": {val: -1023, ok: true},

		"0x0":         {val: 0, ok: true},
		"0x1":         {val: 1, ok: true},
		"0x2":         {val: 2, ok: true},
		"0x3":         {val: 3, ok: true},
		"0x4":         {val: 4, ok: true},
		"0x5":         {val: 5, ok: true},
		"0x6":         {val: 6, ok: true},
		"0x7":         {val: 7, ok: true},
		"0x8":         {val: 8, ok: true},
		"0x9":         {val: 9, ok: true},
		"0xa":         {val: 10, ok: true},
		"0xb":         {val: 11, ok: true},
		"0xc":         {val: 12, ok: true},
		"0xd":         {val: 13, ok: true},
		"0xe":         {val: 14, ok: true},
		"0xf":         {val: 15, ok: true},
		"0xA":         {val: 10, ok: true},
		"0xB":         {val: 11, ok: true},
		"0xC":         {val: 12, ok: true},
		"0xD":         {val: 13, ok: true},
		"0xE":         {val: 14, ok: true},
		"0xF":         {val: 15, ok: true},
		"0xg":         {val: 0, ok: false},
		"0xG":         {val: 0, ok: false},
		"0x10":        {val: 16, ok: true},
		"0x11":        {val: 17, ok: true},
		"0x12":        {val: 18, ok: true},
		"0x100":       {val: 256, ok: true},
		"0x1fF":       {val: 511, ok: true},
		"0xdeadbeef":  {val: 3735928559, ok: true},
		"0xDeAdBeEf":  {val: 3735928559, ok: true},
		"0x1gF":       {val: 0, ok: false},
		"0x1=F":       {val: 0, ok: false},
		"+0x0":        {val: 0, ok: true},
		"+0x1":        {val: 1, ok: true},
		"+0x2":        {val: 2, ok: true},
		"+0x3":        {val: 3, ok: true},
		"+0x4":        {val: 4, ok: true},
		"+0x5":        {val: 5, ok: true},
		"+0x6":        {val: 6, ok: true},
		"+0x7":        {val: 7, ok: true},
		"+0x8":        {val: 8, ok: true},
		"+0x9":        {val: 9, ok: true},
		"+0xa":        {val: 10, ok: true},
		"+0xb":        {val: 11, ok: true},
		"+0xc":        {val: 12, ok: true},
		"+0xd":        {val: 13, ok: true},
		"+0xe":        {val: 14, ok: true},
		"+0xf":        {val: 15, ok: true},
		"+0xA":        {val: 10, ok: true},
		"+0xB":        {val: 11, ok: true},
		"+0xC":        {val: 12, ok: true},
		"+0xD":        {val: 13, ok: true},
		"+0xE":        {val: 14, ok: true},
		"+0xF":        {val: 15, ok: true},
		"+0x10":       {val: 16, ok: true},
		"+0x11":       {val: 17, ok: true},
		"+0x12":       {val: 18, ok: true},
		"+0x100":      {val: 256, ok: true},
		"+0x1fF":      {val: 511, ok: true},
		"+0xdeadbeef": {val: 3735928559, ok: true},
		"+0xDeAdBeEf": {val: 3735928559, ok: true},
		"-0x0":        {val: 0, ok: true},
		"-0x1":        {val: -1, ok: true},
		"-0x2":        {val: -2, ok: true},
		"-0x3":        {val: -3, ok: true},
		"-0x4":        {val: -4, ok: true},
		"-0x5":        {val: -5, ok: true},
		"-0x6":        {val: -6, ok: true},
		"-0x7":        {val: -7, ok: true},
		"-0x8":        {val: -8, ok: true},
		"-0x9":        {val: -9, ok: true},
		"-0xa":        {val: -10, ok: true},
		"-0xb":        {val: -11, ok: true},
		"-0xc":        {val: -12, ok: true},
		"-0xd":        {val: -13, ok: true},
		"-0xe":        {val: -14, ok: true},
		"-0xf":        {val: -15, ok: true},
		"-0xA":        {val: -10, ok: true},
		"-0xB":        {val: -11, ok: true},
		"-0xC":        {val: -12, ok: true},
		"-0xD":        {val: -13, ok: true},
		"-0xE":        {val: -14, ok: true},
		"-0xF":        {val: -15, ok: true},
		"-0x10":       {val: -16, ok: true},
		"-0x11":       {val: -17, ok: true},
		"-0x12":       {val: -18, ok: true},
		"-0x100":      {val: -256, ok: true},
		"-0x1fF":      {val: -511, ok: true},
		"-0xdeadbeef": {val: -3735928559, ok: true},
		"-0xDeAdBeEf": {val: -3735928559, ok: true},
		" +0x1 ":      {val: 1, ok: true},
		" -0x1 ":      {val: -1, ok: true},
		"0x1 ":        {val: 1, ok: true},
		" 0x1":        {val: 1, ok: true},

		" 1 2 ":   {val: 1, ok: true},
		" 1 foo ": {val: 1, ok: true},
		" 1foo ":  {val: 0, ok: false},

		"9223372036854775800":  {val: 9223372036854775800, ok: true},
		"9223372036854775801":  {val: 9223372036854775801, ok: true},
		"9223372036854775802":  {val: 9223372036854775802, ok: true},
		"9223372036854775803":  {val: 9223372036854775803, ok: true},
		"9223372036854775804":  {val: 9223372036854775804, ok: true},
		"9223372036854775805":  {val: 9223372036854775805, ok: true},
		"9223372036854775806":  {val: 9223372036854775806, ok: true},
		"9223372036854775807":  {val: 9223372036854775807, ok: true},
		"9223372036854775808":  {val: 0, ok: false},
		"9223372036854775809":  {val: 0, ok: false},
		"9223372036854775810":  {val: 0, ok: false},
		"9223372036854775811":  {val: 0, ok: false},
		"+9223372036854775800": {val: 9223372036854775800, ok: true},
		"+9223372036854775801": {val: 9223372036854775801, ok: true},
		"+9223372036854775802": {val: 9223372036854775802, ok: true},
		"+9223372036854775803": {val: 9223372036854775803, ok: true},
		"+9223372036854775804": {val: 9223372036854775804, ok: true},
		"+9223372036854775805": {val: 9223372036854775805, ok: true},
		"+9223372036854775806": {val: 9223372036854775806, ok: true},
		"+9223372036854775807": {val: 9223372036854775807, ok: true},
		"+9223372036854775808": {val: 0, ok: false},
		"+9223372036854775809": {val: 0, ok: false},
		"+9223372036854775810": {val: 0, ok: false},
		"+9223372036854775811": {val: 0, ok: false},
		"-9223372036854775800": {val: -9223372036854775800, ok: true},
		"-9223372036854775801": {val: -9223372036854775801, ok: true},
		"-9223372036854775802": {val: -9223372036854775802, ok: true},
		"-9223372036854775803": {val: -9223372036854775803, ok: true},
		"-9223372036854775804": {val: -9223372036854775804, ok: true},
		"-9223372036854775805": {val: -9223372036854775805, ok: true},
		"-9223372036854775806": {val: -9223372036854775806, ok: true},
		"-9223372036854775807": {val: -9223372036854775807, ok: true},
		"-9223372036854775808": {val: -9223372036854775808, ok: true},
		"-9223372036854775809": {val: 0, ok: false},
		"-9223372036854775810": {val: 0, ok: false},
		"-9223372036854775820": {val: 0, ok: false},

		"0777777777777777777770":  {val: 9223372036854775800, ok: true},
		"0777777777777777777771":  {val: 9223372036854775801, ok: true},
		"0777777777777777777772":  {val: 9223372036854775802, ok: true},
		"0777777777777777777773":  {val: 9223372036854775803, ok: true},
		"0777777777777777777774":  {val: 9223372036854775804, ok: true},
		"0777777777777777777775":  {val: 9223372036854775805, ok: true},
		"0777777777777777777776":  {val: 9223372036854775806, ok: true},
		"0777777777777777777777":  {val: 9223372036854775807, ok: true},
		"01000000000000000000000": {val: 0, ok: false},
		"01000000000000000000001": {val: 0, ok: false},
		"01000000000000000000002": {val: 0, ok: false},

		"-0777777777777777777777": {val: -9223372036854775807,
			ok: true},
		"-01000000000000000000000": {val: -9223372036854775808,
			ok: true},

		"-01000000000000000000001": {val: 0, ok: false},
		"-01000000000000000000002": {val: 0, ok: false},
		"-01000000000000000000003": {val: 0, ok: false},

		"0x7FFFFFFFFFFFFFF0":  {val: 9223372036854775792, ok: true},
		"0x7FFFFFFFFFFFFFF1":  {val: 9223372036854775793, ok: true},
		"0x7FFFFFFFFFFFFFF2":  {val: 9223372036854775794, ok: true},
		"0x7FFFFFFFFFFFFFF3":  {val: 9223372036854775795, ok: true},
		"0x7FFFFFFFFFFFFFF4":  {val: 9223372036854775796, ok: true},
		"0x7FFFFFFFFFFFFFF5":  {val: 9223372036854775797, ok: true},
		"0x7FFFFFFFFFFFFFF6":  {val: 9223372036854775798, ok: true},
		"0x7FFFFFFFFFFFFFF7":  {val: 9223372036854775799, ok: true},
		"0x7FFFFFFFFFFFFFF8":  {val: 9223372036854775800, ok: true},
		"0x7FFFFFFFFFFFFFF9":  {val: 9223372036854775801, ok: true},
		"0x7FFFFFFFFFFFFFFA":  {val: 9223372036854775802, ok: true},
		"0x7FFFFFFFFFFFFFFB":  {val: 9223372036854775803, ok: true},
		"0x7FFFFFFFFFFFFFFC":  {val: 9223372036854775804, ok: true},
		"0x7FFFFFFFFFFFFFFD":  {val: 9223372036854775805, ok: true},
		"0x7FFFFFFFFFFFFFFE":  {val: 9223372036854775806, ok: true},
		"0x7FFFFFFFFFFFFFFF":  {val: 9223372036854775807, ok: true},
		"0x8000000000000000":  {val: 0, ok: false},
		"0x8000000000000001":  {val: 0, ok: false},
		"0x8000000000000002":  {val: 0, ok: false},
		"-0x7FFFFFFFFFFFFFFE": {val: -9223372036854775806, ok: true},
		"-0x7FFFFFFFFFFFFFFF": {val: -9223372036854775807, ok: true},
		"-0x8000000000000000": {val: -9223372036854775808, ok: true},
		"-0x8000000000000001": {val: 0, ok: false},
		"-0x8000000000000002": {val: 0, ok: false},
	}

	for str, exp := range expMap {
		val, ok := parseInt64([]byte(str))
		if val != exp.val || ok != exp.ok {
			t.Errorf("parseInt64(%s) want=%v got={%v %v}", str, exp,
				val, ok)
		}
	}
}

func BenchmarkParseInt64(b *testing.B) {
	s := []byte("9223372036854775807")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := parseInt64(s)
		if !ok {
			b.Fatal("parseInt64() failed to parse")
			return
		}
	}
}

func BenchmarkStrconvParseInt(b *testing.B) {
	s := []byte("9223372036854775807")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = strconv.ParseInt(string(s), 0, 64)
	}
}

func TestParseFloat64(t *testing.T) {
	expMap := map[string]struct {
		val float64
		ok  bool
	}{
		"":      {val: 0., ok: false},
		"0":     {val: 0., ok: true},
		"1":     {val: 1., ok: true},
		"0.":    {val: 0., ok: true},
		"1.":    {val: 1., ok: true},
		"0.0":   {val: 0., ok: true},
		"1.0":   {val: 1., ok: true},
		" 1.0":  {val: 1., ok: true},
		"1.0 ":  {val: 1., ok: true},
		" 1.0 ": {val: 1., ok: true},
		"foo":   {val: 0., ok: false},
		" foo":  {val: 0., ok: false},

		// tests from Varnish vnum.c
		"12":        {val: 12., ok: true},
		"12.":       {val: 12., ok: true},
		"12.3":      {val: 12.3, ok: true},
		"12.34":     {val: 12.34, ok: true},
		"12.34e-3":  {val: 12.34e-3, ok: true},
		"12.34e3":   {val: 12.34e3, ok: true},
		"12.34e+3":  {val: 12.34e3, ok: true},
		"+12.34e-3": {val: 12.34e-3, ok: true},
		"-12.34e3":  {val: -12.34e3, ok: true},
		".":         {val: 0., ok: false},
		".12.":      {val: 0., ok: false},
		"12..":      {val: 0., ok: false},
		"12.,":      {val: 0., ok: false},
		"12e,":      {val: 0., ok: false},
		"12e+,":     {val: 0., ok: false},
		"12ee,":     {val: 0., ok: false},
		"1..2":      {val: 0., ok: false},
		"A":         {val: 0., ok: false},
		"1A":        {val: 0., ok: false},
		"e-3":       {val: 0., ok: false},

		"-0":     {val: 0., ok: true},
		"-1":     {val: -1., ok: true},
		"-0.":    {val: 0., ok: true},
		"-1.":    {val: -1., ok: true},
		"-0.0":   {val: 0., ok: true},
		"-1.0":   {val: -1., ok: true},
		" -1.0":  {val: -1., ok: true},
		"-1.0 ":  {val: -1., ok: true},
		" -1.0 ": {val: -1., ok: true},
		"-foo":   {val: 0., ok: false},
		" -foo":  {val: 0., ok: false},

		// Varnish log timestamps: epoch and duration in us.
		"1536134634.139056": {val: 1536134634.139056, ok: true},
		"0.000001":          {val: 0.000001, ok: true},

		// DBL_EPSILON with one fewer digit before 'e'
		"2.220446049250313e-016": {val: 2.220446049250313e-016, ok: true},

		// +-2^53
		"9.007199254740992e15":  {val: 9.007199254740992e15, ok: true},
		"-9.007199254740992e15": {val: -9.007199254740992e15, ok: true},
	}

	for str, exp := range expMap {
		val, ok := parseFloat64([]byte(str))
		if ok != exp.ok || val != exp.val {
			t.Errorf("parseFloat64(%s) want=%v got={%v %v}", str,
				exp, val, ok)
		}
	}
}

var fltBenchVec = []struct {
	name  string
	bytes []byte
}{
	{"pow2mantissa", []byte("9.007199254740992e15")},
	{"epochMicro", []byte("1536134634.139056")},
	{"durationMicro", []byte("0.000001")},
}

func BenchmarkParseFloat64(b *testing.B) {
	for _, v := range fltBenchVec {
		b.Run(v.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, ok := parseFloat64(v.bytes)
				if !ok {
					b.Fatal("parseFloat64() failed")
					return
				}
			}
		})
	}
}

func BenchmarkStrconvParseFloat(b *testing.B) {
	for _, v := range fltBenchVec {
		b.Run(v.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = strconv.ParseFloat(string(v.bytes), 64)
			}
		})
	}
}
