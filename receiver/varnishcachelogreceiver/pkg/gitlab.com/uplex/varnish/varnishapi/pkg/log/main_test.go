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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	testFile = "testdata/bin.log"
	vxidLog  = "testdata/vxid.log"
	reqLog   = "testdata/request.log"
	sessLog  = "testdata/session.log"
	rawLog   = "testdata/raw.log"
	l1Log    = "testdata/l00001.log"
	failPath = "ifAFileWithThisNameReallyExistsThenTestsFail"
	target   = "http://localhost:8080"
)

type testRec struct {
	vxid    int
	tag     string
	rectype rune
	payload string
}

type testTx struct {
	level  uint
	vxid   uint32
	txtype string
	recs   []testRec
}

var (
	txline     = regexp.MustCompile(`^(\S+)\s+<<\s+(\w+)\s+>>\s+(\d+)`)
	recline    = regexp.MustCompile(`^\S+\s+(\d+)\s+(\w+)\s+([b|c|-])\s+(.*)$`)
	begin      = regexp.MustCompile(`^\w+\s+(\d+)\s+(\S+)$`)
	rawLine    = regexp.MustCompile(`^\s+(\d+)\s+(\w+)\s+([b|c|-])\s+(.*)$`)
	lvls13     = regexp.MustCompile(`^\*+$`)
	lvlsGe4    = regexp.MustCompile(`^\*+(\d+)\*+$`)
	expVxidLog [][]testTx
	expReqLog  [][]testTx
	expSessLog [][]testTx
	expRawLog  []testRec
	twoNLs     = []byte("\n\n")
	nlStar     = []byte("\n*")
)

func url(path string) string {
	return target + path
}

// Split function for bufio.Scanner to scrape tx groups
func txGrpSplit(data []byte, atEOF bool) (int, []byte, error) {
	if !bytes.Contains(data, twoNLs) {
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
	i := bytes.Index(data, twoNLs)
	if i < 0 {
		return 0, nil, errors.New("should have found two newlines")
	}
	return i + 2, data[:i], nil
}

// Split function for bufio.Scanner to scrape a tx from the bytes
// returned by the tx group scanner.
func txSplit(data []byte, atEOF bool) (int, []byte, error) {
	if !bytes.Contains(data, nlStar) {
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
	i := bytes.Index(data, nlStar)
	if i < 0 {
		return 0, nil, errors.New("should have found '\\n'*'")
	}
	return i + 1, data[:i], nil
}

func scrapeTx(txBytes []byte) (testTx, error) {
	tx := testTx{}
	rdr := bytes.NewReader(txBytes)
	lines := bufio.NewScanner(rdr)
	for lines.Scan() {
		flds := txline.FindStringSubmatch(lines.Text())
		if flds == nil {
			return tx, errors.New("Cannot parse tx line: " +
				lines.Text())
		}
		if lvls13.MatchString(flds[1]) {
			tx.level = uint(len(flds[1]))
		} else {
			lvlStr := lvlsGe4.FindStringSubmatch(flds[1])
			if lvlStr == nil {
				return tx, fmt.Errorf("Cannot parse level "+
					"from %s in tx line: %s", flds[1],
					lines.Text())
			}
			lvl, err := strconv.Atoi(lvlStr[1])
			if err != nil {
				return tx, fmt.Errorf("Cannot parse level "+
					"from %s in tx line: %s", flds[1],
					lines.Text())
			}
			tx.level = uint(lvl)
		}
		txVxid, err := strconv.Atoi(flds[3])
		if err != nil {
			return tx, errors.New("Cannot parse vxid " +
				flds[4] + " in tx line: " + lines.Text())
		}
		tx.vxid = uint32(txVxid)
		tx.txtype = flds[2]
		for lines.Scan() {
			if lines.Text() == "" {
				return tx, nil
			}
			flds = recline.FindStringSubmatch(lines.Text())
			if flds == nil {
				return tx, errors.New("Cannot parse record: " +
					lines.Text())
			}
			rec := testRec{}
			rec.vxid, err = strconv.Atoi(flds[1])
			if err != nil {
				return tx, errors.New("Cannot parse vxid " +
					flds[1] + " in rec line: " +
					lines.Text())
			}
			rec.tag = flds[2]
			rec.rectype = rune(flds[3][0])
			rec.payload = flds[4]
			tx.recs = append(tx.recs, rec)
		}
	}
	return tx, nil
}

// Scrape a log file with transactional data written by varnishlog in
// verbose mode.
func scrapeLog(file string) ([][]testTx, error) {
	var txGrps [][]testTx
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	txGrpScanner := bufio.NewScanner(f)
	txGrpScanner.Split(txGrpSplit)
	for txGrpScanner.Scan() {
		var txGrp []testTx
		txGrpBytes := txGrpScanner.Bytes()
		if len(txGrpBytes) == 0 {
			break
		}
		txRdr := bytes.NewReader(txGrpBytes)
		txScanner := bufio.NewScanner(txRdr)
		txScanner.Split(txSplit)
		for txScanner.Scan() {
			txBytes := txScanner.Bytes()
			if len(txBytes) == 0 {
				break
			}
			tx, err := scrapeTx(txBytes)
			if err != nil {
				return nil, err
			}
			txGrp = append(txGrp, tx)
		}
		txGrps = append(txGrps, txGrp)
	}
	return txGrps, nil
}

// Scrape a log file with raw transactions written by varnishlog in
// verbose mode.
func scrapeRawLog(file string) ([]testRec, error) {
	var testRecs []testRec
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lines := bufio.NewScanner(f)

	for lines.Scan() {
		flds := rawLine.FindStringSubmatch(lines.Text())
		if flds == nil {
			return nil, errors.New("Cannot parse line: " +
				lines.Text())
		}
		vxid, err := strconv.Atoi(flds[1])
		if err != nil {
			return nil, errors.New("Cannot parse vxid " + flds[1] +
				" in line: " + lines.Text())
		}
		rec := testRec{
			vxid:    vxid,
			tag:     flds[2],
			rectype: rune(flds[3][0]),
			payload: flds[4],
		}
		testRecs = append(testRecs, rec)
	}
	return testRecs, nil
}

func checkRecord(t *testing.T, rec Record, expRec testRec) {
	if rec.VXID != uint(expRec.vxid) {
		t.Errorf("rec vxid expected=%v got=%v", expRec.vxid, rec.VXID)
	}
	if rec.Tag.String() != expRec.tag {
		t.Errorf("rec tag expected=%v got=%s", expRec.tag, rec.Tag)
	}
	if rune(rec.Type) != expRec.rectype {
		t.Errorf("rec type expected=%v got=%v", expRec.rectype,
			rune(rec.Type))
	}
	if len(rec.Payload) != len(expRec.payload) {
		t.Errorf("rec payload length expected=%v got=%v",
			len(expRec.payload), len(rec.Payload))
	}
	if string(rec.Payload) != expRec.payload {
		t.Errorf("rec payload expected='%v' got='%v'", expRec.payload,
			string(rec.Payload))
	}
}

func checkExpTx(t *testing.T, tx Tx, expTx testTx) {
	// if tx.Level != expTx.level {
	// 	t.Errorf("tx Level expected=%v got=%v", expTx.level, tx.Level)
	// }
	if tx.VXID != expTx.vxid {
		t.Errorf("tx VXID expected=%v got=%v", expTx.vxid, tx.VXID)
	}
	if tx.Type.String() != expTx.txtype {
		t.Errorf("tx Type expected=%v got=%v", expTx.txtype,
			tx.Type.String())
	}
	if tx.Type != TxRaw {
		if len(expTx.recs) == 0 {
			t.Error("scraped tx has no records")
			return
		}
		if expTx.recs[0].tag != "Begin" {
			t.Error("scraped tx does not begin with Begin")
		}
		payload := expTx.recs[0].payload
		flds := begin.FindStringSubmatch(payload)
		if len(flds) != 3 {
			t.Errorf("could not parse Begin: " + payload)
			return
		}
		if tx.Reason.String() != flds[2] {
			t.Errorf("tx reason expected=%v got=%s", flds[2],
				tx.Reason)
		}
		// XXX test parent vxid in grouped txn
	}

	if len(tx.Records) != len(expTx.recs) {
		t.Errorf("tx number of records expected=%v got=%v",
			len(expTx.recs), len(tx.Records))
		return
	}
	for i, rec := range tx.Records {
		expRec := expTx.recs[i]
		checkRecord(t, rec, expRec)
	}
}

func checkExpTxGroup(t *testing.T, txGrp []Tx, expTxGrp []testTx) {
	if len(txGrp) != len(expTxGrp) {
		t.Fatalf("number of tx expected in group want=%v got=%v",
			len(expTxGrp), len(txGrp))
		return
	}
	for i, tx := range txGrp {
		checkExpTx(t, tx, expTxGrp[i])
	}
}

func checkTxGroups(t *testing.T, txGrps [][]Tx, expTxGrps [][]testTx) {
	if len(txGrps) != len(expTxGrps) {
		t.Fatalf("number of transaction groups want=%v got=%v",
			len(expTxGrps), len(txGrps))
		return
	}
	for i, txGrp := range txGrps {
		if len(txGrp) != len(expTxGrps[i]) {
			t.Fatalf("transactions in group want=%v got=%v",
				len(expTxGrps[i]), len(txGrp))
		}
		checkExpTxGroup(t, txGrp, expTxGrps[i])
	}
}

func checkBereq(t *testing.T, tx Tx, req http.Request) {
	if tx.Type != BeReq {
		t.Fatalf("checkBereq() tx.Type want=BeReq got=%s", tx.Type)
		t.FailNow()
		return
	}
	for _, rec := range tx.Records {
		if rec.Tag.String() != "BereqURL" {
			continue
		}
		if string(rec.Payload) != req.URL.Path {
			t.Errorf("bereq URL want=%v got=%v", req.URL.Path,
				string(rec.Payload))
		}
		break
	}
}

func checkReqResp(t *testing.T, tx Tx, req http.Request, resp http.Response) {
	if tx.Type != Req {
		t.Fatalf("checkReqResp() tx.Type want=Req got=%s", tx.Type)
		t.FailNow()
		return
	}
	respHdr := make(http.Header)
	for _, rec := range tx.Records {
		switch rec.Tag.String() {
		case "ReqMethod":
			if string(rec.Payload) != req.Method {
				t.Errorf("request method want=%v got=%v",
					req.Method, string(rec.Payload))
			}
		case "ReqURL":
			if string(rec.Payload) != req.URL.Path {
				t.Errorf("request URL want=%v got=%v",
					req.URL.Path, string(rec.Payload))
			}
		case "ReqProtocol":
			if string(rec.Payload) != req.Proto {
				t.Errorf("request protocol want=%v got=%v",
					req.Proto, string(rec.Payload))
			}
		case "ReqHeader":
			if !strings.HasPrefix(string(rec.Payload), "Host:") {
				continue
			}
			host := string(rec.Payload)[len("Host: "):]
			if host != req.Host {
				t.Errorf("request Host want=%v got=%v",
					req.Host, host)
			}
		case "RespProtocol":
			if string(rec.Payload) != resp.Proto {
				t.Errorf("response protocol want=%v got=%v",
					resp.Proto, string(rec.Payload))
			}
		case "RespStatus":
			expStatus := strconv.FormatInt(int64(resp.StatusCode),
				10)
			if string(rec.Payload) != expStatus {
				t.Errorf("response status want=%d got=%v",
					resp.StatusCode, string(rec.Payload))
			}
		case "RespReason":
			expReason := resp.Status[4:]
			if string(rec.Payload) != expReason {
				t.Errorf("response reason want=%v got=%v",
					expReason, string(rec.Payload))
			}
		case "RespHeader":
			colonIdx := strings.Index(string(rec.Payload), ": ")
			if colonIdx < 0 {
				t.Fatal("cannot parse response header:",
					string(rec.Payload))
				continue
			}
			hdr := string(rec.Payload)[:colonIdx]
			val := string(rec.Payload)[colonIdx+2:]
			respHdr.Set(hdr, val)
		default:
			continue
		}
	}
	// http.Response apparently removes the Connection header, but
	// sets its Close field to true if it was present and set to
	// "close".
	if resp.Close {
		conn := respHdr.Get("Connection")
		if conn != "close" {
			t.Errorf("response Connection:close not found")
		}
		if conn != "" {
			respHdr.Del("Connection")
		}
	}
	// Won't worry about multiple values for a header
	if len(resp.Header) != len(respHdr) {
		t.Errorf("response headers want=%v got=%v", len(resp.Header),
			len(respHdr))
		return
	}
	for h := range resp.Header {
		if respHdr.Get(h) != resp.Header.Get(h) {
			t.Errorf("response header %v want=%v got=%v", h,
				resp.Header.Get(h), respHdr.Get(h))
		}
	}
}

func TestMain(m *testing.M) {
	files := []string{testFile, vxidLog, rawLog, reqLog, sessLog, l1Log}
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot stat "+file+":", err)
			os.Exit(1)
		}
	}

	var err error
	if expVxidLog, err = scrapeLog(vxidLog); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse "+vxidLog+":", err)
		os.Exit(1)
	}
	if expReqLog, err = scrapeLog(reqLog); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse "+reqLog+":", err)
		os.Exit(1)
	}
	if expSessLog, err = scrapeLog(sessLog); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse "+sessLog+":", err)
		os.Exit(1)
	}
	if expRawLog, err = scrapeRawLog(rawLog); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot parse "+rawLog+":", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
