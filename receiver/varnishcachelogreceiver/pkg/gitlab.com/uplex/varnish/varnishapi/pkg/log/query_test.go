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

import "testing"

func TestNewQuery(t *testing.T) {
	l := New()
	defer l.Release()

	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
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

	var n *Cursor
	if _, err := n.NewQuery(GRaw, ""); err == nil {
		t.Error("Expected nil.NewQuery() to fail")
	}
	uninit := &Cursor{}
	if _, err := uninit.NewQuery(GRaw, ""); err == nil {
		t.Error("Expected uninitiailized.NewQuery() to fail")
	}
}

var expFirstTx = testTx{
	level:  0,
	txtype: "Record",
	vxid:   0,
	recs:   []testRec{expFirstRec},
}

func TestNextTxGroupRawOne(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(GRaw, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	txn, status := q.NextTxGroup()
	if status != More {
		t.Errorf("NextTxGroup() status want=More got=%d\n",
			uint8(status))
	}
	if len(txn) != 1 {
		t.Errorf("len(Txn) from NextTxGroup want=1 got=%v\n",
			len(txn))
	}
	tx := txn[0]
	checkExpTx(t, tx, expFirstTx)
	if tx.Reason != ReasonUnknown {
		t.Errorf("tx Reason want=unknown got=%v\n", tx.Reason)
	}
	if tx.ParentVXID != 0 {
		t.Errorf("tx ParentVXID want=0 got=%v\n", tx.ParentVXID)
	}
}

func TestNextTxGroupRawAll(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(GRaw, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	for _, rec := range expRawLog {
		txn, status := q.NextTxGroup()
		if status != More {
			t.Errorf("NextTxGroup() status want=More got=%d\n",
				uint8(status))
		}
		if len(txn) != 1 {
			t.Errorf("len(Txn) from NextTxGroup want=1 got=%v\n",
				len(txn))
		}
		tx := txn[0]
		expTx := testTx{
			level:  0,
			txtype: "Record",
			vxid:   uint32(rec.vxid),
			recs:   []testRec{rec},
		}
		checkExpTx(t, tx, expTx)
		if tx.Reason != ReasonUnknown {
			t.Errorf("tx Reason want=unknown got=%v\n", tx.Reason)
		}
		if tx.ParentVXID != 0 {
			t.Errorf("tx ParentVXID want=0 got=%v\n", tx.ParentVXID)
		}
	}
}

func TestNextTxGroupDefault(t *testing.T) {
	var txGrps [][]Tx

	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(VXID, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	var status Status
	for {
		txn, rdstatus := q.NextTxGroup()
		if rdstatus != More {
			status = rdstatus
			break
		}
		txGrps = append(txGrps, txn)
	}

	if status != Status(EOF) {
		t.Errorf("expected EOF status got: %s", status.Error())
	}
	checkTxGroups(t, txGrps, expVxidLog)
}

func TestNextTxGroupConcurrent(t *testing.T) {
	var txGrps1, txGrps2 [][]Tx

	l := New()
	defer l.Release()
	c1, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c1.Delete()
	q1, err := c1.NewQuery(VXID, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}
	c2, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c2.Delete()
	q2, err := c2.NewQuery(VXID, "")
	if err != nil {
		t.Fatal("NewQuery():", err)
		return
	}

	for {
		txGrp1, status1 := q1.NextTxGroup()
		txGrp2, status2 := q2.NextTxGroup()
		if status1 == EOF && status2 == EOF {
			break
		}
		if status1 != More {
			t.Fatal("NextTxGroup() unexpected status:", status1)
			return
		}
		if status2 != More {
			t.Fatal("NextTxGroup() unexpected status:", status2)
			return
		}
		txGrps1 = append(txGrps1, txGrp1)
		txGrps2 = append(txGrps2, txGrp2)
	}

	checkTxGroups(t, txGrps1, expVxidLog)
	checkTxGroups(t, txGrps2, expVxidLog)
}

func TestNextTxGroupRequestOne(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(Request, "")
	if err != nil {
		t.Fatal("NewQuery(Request):", err)
		return
	}

	txGrp, status := q.NextTxGroup()
	if status != More {
		t.Errorf("NextTxGroup() status want=More got=%d\n",
			uint8(status))
	}
	checkExpTxGroup(t, txGrp, expReqLog[0])
}

func TestNextTxGroupRequestAll(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(Request, "")
	if err != nil {
		t.Fatal("NewQuery(Request):", err)
		return
	}

	var txGrps [][]Tx
	var status Status
	for {
		txGrp, rdstatus := q.NextTxGroup()
		if rdstatus != More {
			status = rdstatus
			break
		}
		txGrps = append(txGrps, txGrp)
	}
	if status != EOF {
		t.Errorf("NextTxGroup() status want=More got=%d\n",
			uint8(status))
	}
	checkTxGroups(t, txGrps, expReqLog)
}

func TestNextTxGroupSessionOne(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(Session, "")
	if err != nil {
		t.Fatal("NewQuery(Session):", err)
		return
	}

	txGrp, status := q.NextTxGroup()
	if status != More {
		t.Errorf("NextTxGroup() status want=More got=%d\n",
			uint8(status))
	}
	checkExpTxGroup(t, txGrp, expSessLog[0])
}

func TestNextTxGroupSessionAll(t *testing.T) {
	l := New()
	defer l.Release()
	c, err := l.NewCursorFile(testFile)
	if err != nil {
		t.Fatal("NewCursorFile(bin.log):", err)
		return
	}
	defer c.Delete()
	q, err := c.NewQuery(Session, "")
	if err != nil {
		t.Fatal("NewQuery(Session):", err)
		return
	}

	var txGrps [][]Tx
	var status Status
	for {
		txGrp, rdstatus := q.NextTxGroup()
		if rdstatus != More {
			status = rdstatus
			break
		}
		txGrps = append(txGrps, txGrp)
	}
	if status != EOF {
		t.Errorf("NextTxGroup() status want=More got=%d\n",
			uint8(status))
	}
	checkTxGroups(t, txGrps, expSessLog)
}

// Test vectors in query_eval_test.go (mostly adapted from l00001.vtc)
func TestNextTxGroupQuery(t *testing.T) {
	l := New()
	defer l.Release()
	vec := queryRecTestVec
	vec = append(vec, queryTxTestVec...)
	vec = append(vec, queryVxidTestVec...)
	for _, v := range vec {
		if v.fail == "fail" {
			continue
		}
		t.Run(v.name, func(t *testing.T) {
			c, err := l.NewCursorFile(l1Log)
			if err != nil {
				t.Fatal("NewCursorFile(l00001.log):", err)
				return
			}
			defer c.Delete()
			q, err := c.NewQuery(VXID, v.query)
			if err != nil {
				t.Fatal("NewQuery(VXID):", err)
				return
			}
			txGrp, status := q.NextTxGroup()
			if status != More {
				t.Fatalf("NextTxGroup() status want=More got=%v",
					status)
				return
			}
			if len(txGrp) != 1 {
				t.Errorf("len(txGrp) want=1 got=%v", len(txGrp))
				if len(txGrp) == 0 {
					return
				}
			}
			if txGrp[0].VXID != 118200424 {
				t.Errorf("tx.VXID want=118200424 got=%v",
					txGrp[0].VXID)
			}
		})
	}
}
