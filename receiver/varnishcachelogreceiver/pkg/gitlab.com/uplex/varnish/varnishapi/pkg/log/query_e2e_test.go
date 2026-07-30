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
package log

import (
	"net/http"
	"testing"
)

// XXX remove the duplication with the test helpers in main_test.go

var tx2recType = map[TxType]RecordType{
	Sess:  Client,
	Req:   Client,
	BeReq: Backend,
}

const (
	undefLen        int        = int(^uint(0) >> 1)
	txTypeUndef     TxType     = TxUnknown
	txReasonUndef   Reason     = ReasonUnknown
	txLevelUndef    uint       = ^uint(0)
	txVXIDUndef     uint32     = ^uint32(0)
	recTypeUndef    RecordType = RecordType(0)
	recTagUndef     string     = "undefined tag string"
	recVXIDUndef    uint       = ^uint(0)
	recPayloadUndef string     = "undefined payload string"
)

type expRec struct {
	tag     string
	payload string
}

type expTx struct {
	typ    TxType
	reason Reason
	level  uint
	vxid   uint32
	pvxid  uint32
	recs   []expRec
}

type expTxLen struct {
	expRecs int
	tx      expTx
}

type expTxGrp struct {
	expTx int
	txGrp []expTxLen
}

var expDefaultRead = map[TxType]expTxGrp{
	BeReq: {
		expTx: 1,
		txGrp: []expTxLen{{
			expRecs: undefLen,
			tx: expTx{
				typ:    BeReq,
				reason: Fetch,
				level:  1,
				vxid:   txVXIDUndef,
				pvxid:  txVXIDUndef,
				recs:   nil,
			}}},
	},
	Req: {
		expTx: 1,
		txGrp: []expTxLen{{
			expRecs: undefLen,
			tx: expTx{
				typ:    Req,
				reason: RxReq,
				level:  1,
				vxid:   txVXIDUndef,
				pvxid:  txVXIDUndef,
				recs:   nil,
			}}},
	},
	Sess: {
		expTx: 1,
		txGrp: []expTxLen{{
			expRecs: 5,
			tx: expTx{
				typ:    Sess,
				reason: HTTP1,
				level:  1,
				vxid:   txVXIDUndef,
				pvxid:  0,
				recs: []expRec{
					{
						tag:     "Begin",
						payload: "sess 0 HTTP/1",
					},
					{
						tag:     "SessOpen",
						payload: recPayloadUndef,
					},
					{
						tag:     "Link",
						payload: recPayloadUndef,
					},
					{
						tag:     "SessClose",
						payload: recPayloadUndef,
					},
					{
						tag:     "End",
						payload: "",
					},
				}},
		}},
	},
}

func checkRec(t *testing.T, rec Record, expRec expRec) {
	if expRec.tag != recTagUndef && rec.Tag.String() != expRec.tag {
		t.Errorf("record tag want=%v got=%s", expRec.tag, rec.Tag)
	}
	if expRec.payload != recPayloadUndef &&
		string(rec.Payload) != expRec.payload {
		t.Errorf("record payload want=%v got=%s", expRec.payload,
			rec.Payload)
	}
}

func checkTx(t *testing.T, tx Tx, expTxLen expTxLen) {
	expTx := expTxLen.tx
	if expTx.typ != txTypeUndef && expTx.typ != tx.Type {
		t.Errorf("tx.Type want=%v got=%v", expTx.typ, tx.Type)
	}
	if expTx.reason != txReasonUndef && expTx.reason != tx.Reason {
		t.Errorf("tx.Reason want=%v got=%v", expTx.reason, tx.Reason)
	}
	if expTx.level != txLevelUndef && expTx.level != tx.Level {
		t.Errorf("tx.Level want=%v got=%v", expTx.level, tx.Level)
	}
	if expTx.vxid != txVXIDUndef && expTx.vxid != tx.VXID {
		t.Errorf("tx.VXID want=%v got=%v", expTx.vxid, tx.VXID)
	}
	if expTx.pvxid != txVXIDUndef && expTx.pvxid != tx.ParentVXID {
		t.Errorf("tx.ParentVXID want=%v got=%v", expTx.pvxid,
			tx.ParentVXID)
	}
	if expTxLen.expRecs != undefLen && expTxLen.expRecs != len(tx.Records) {
		t.Errorf("number of records want=%v got=%v", expTxLen.expRecs,
			len(tx.Records))
	}
	if tx.Records == nil || len(tx.Records) < 1 {
		t.Fatal("tx has no records:", tx)
		return
	}
	if tx.Type == TxRaw {
		if len(tx.Records) != 1 {
			t.Errorf("tx type raw number of records want=1 got=%v",
				len(tx.Records))
		}
		if tx.Records[0].VXID != uint(tx.VXID) {
			t.Errorf("record VXID want=%v got=%v", uint(tx.VXID),
				tx.Records[0].VXID)
		}
	} else {
		// These conditions always hold for the records in a
		// non-raw transaction.
		if tx.Records[0].Tag.String() != "Begin" {
			t.Errorf("tx first record tag want=Begin got=%s",
				tx.Records[0].Tag)
		}
		lastIdx := len(tx.Records) - 1
		if tx.Records[lastIdx].Tag.String() != "End" {
			t.Errorf("tx last record tag want=End got=%s",
				tx.Records[lastIdx].Tag)
		}
		if string(tx.Records[lastIdx].Payload) != "" {
			t.Errorf("tx last record tag want=%q got=%q", "",
				tx.Records[lastIdx].Payload)
		}
		expType, ok := tx2recType[tx.Type]
		if !ok {
			t.Fatal("tx2recType no value for", tx.Type)
			t.FailNow()
			return
		}
		for _, rec := range tx.Records {
			if rec.Type != expType {
				t.Errorf("record type want=%c got=%c", expType,
					rec.Type)
			}
			if rec.VXID != uint(tx.VXID) {
				t.Errorf("record VXID want=%v got=%v",
					uint(tx.VXID), rec.VXID)
			}
		}
	}
	if expTx.recs != nil {
		for i, rec := range tx.Records {
			checkRec(t, rec, expTx.recs[i])
		}
	}
}

func checkTxGrp(t *testing.T, txGrp []Tx, expTxGrp expTxGrp) {
	if expTxGrp.expTx != undefLen && len(txGrp) != expTxGrp.expTx {
		t.Fatalf("number of tx in group got=%v want=%v",
			len(txGrp), expTxGrp.expTx)
		return
	}
	for i, tx := range txGrp {
		checkTx(t, tx, expTxGrp.txGrp[i])
	}
}

func TestAttachQuery(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	c, err := l.NewCursor()
	if err != nil {
		t.Fatal("NewCursor():", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(GRaw, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}
	if q == nil {
		t.Fatal("NewQuery() returned nil")
		return
	}

	ln := New()
	defer ln.Release()
	if err := ln.Attach("gotest"); err != nil {
		t.Fatal("Attach(gotest):", err)
		return
	}
	cn, err := ln.NewCursor()
	if err != nil {
		t.Fatal("NewCursor() for gotest:", err)
		return
	}
	defer cn.Delete()
	qn, err := cn.NewQuery(GRaw, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}
	if qn == nil {
		t.Fatal("NewQuery() for gotest returned nil")
		return
	}
}

func TestE2EQueryRaw(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	c, err := l.NewCursor()
	if err != nil {
		t.Fatal("NewCursor():", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(GRaw, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", url("/uncacheable"), nil)
	if err != nil {
		t.Fatal("/uncacheable Request", err)
	}
	req.Close = true
	stopChan := make(chan bool)
	respChan := make(chan bool)
	go func() {
		sent := false
		for {
			select {
			case <-stopChan:
				return
			default:
				_, err := client.Do(req)
				if err != nil {
					t.Fatal("GET request:", err)
					return
				}
				if !sent {
					respChan <- true
					sent = true
				}
			}
		}
	}()

	<-respChan
	var txGrps [][]Tx
	for i := 0; i < 100; i++ {
		txGrp, status := q.NextTxGroup()
		txGrps = append(txGrps, txGrp)
		if status == EOL {
			break
		}
	}
	stopChan <- true
	t.Logf("read %d txGrps", len(txGrps))
	t.Log(txGrps)
}

func TestE2EQueryVXID(t *testing.T) {
	l := New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		t.Fatal("Attach(default):", err)
		return
	}
	c, err := l.NewCursor()
	if err != nil {
		t.Fatal("NewCursor():", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(VXID, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	var status Status
	var txGrps [][]Tx
	stopChan := make(chan bool)
	go func() {
		for len(txGrps) < 3 {
			txGrp, txStatus := q.NextTxGroup()
			status = txStatus
			if txStatus == EOL {
				continue
			}
			if txStatus != More {
				break
			}
			txGrps = append(txGrps, txGrp)
		}
		stopChan <- true
	}()

	client := &http.Client{}
	req, err := http.NewRequest("GET", url("/uncacheable"), nil)
	if err != nil {
		t.Fatal("/uncacheable Request", err)
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("/uncacheable Response", err)
	}

	<-stopChan
	if status != More && status != EOL {
		t.Error("Read status:", status)
	}
	if len(txGrps) != 3 {
		t.Errorf("number of tx groups want=3 got=%v", len(txGrps))
	}
	for _, txGrp := range txGrps {
		tx := txGrp[0]
		checkTxGrp(t, txGrp, expDefaultRead[tx.Type])
		if tx.Type == BeReq {
			checkBereq(t, tx, *req)
		}
		if tx.Type == Req {
			checkReqResp(t, tx, *req, *resp)
		}
	}
}
