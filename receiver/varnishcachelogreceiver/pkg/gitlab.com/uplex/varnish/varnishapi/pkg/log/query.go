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

import "slices"

/*
#cgo pkg-config: varnishapi
#include <stdio.h>
#include <vapi/vsl.h>
*/
import "C"

import (
	"bytes"
	"errors"
)

type existence struct{}

var (
	exister      = struct{}{}
	sessBytes    = []byte("ess")
	reqBytes     = []byte("eq")
	bereqBytes   = []byte("ereq")
	http1Bytes   = []byte("TTP/1")
	rxreqBytes   = []byte("xreq")
	esiBytes     = []byte("si")
	restartBytes = []byte("estart")
	passBytes    = []byte("ass")
	fetchBytes   = []byte("etch")
	bgFetchBytes = []byte("gfetch")
	pipeBytes    = []byte("ipe")
)

func bytes2Type(b []byte) (TxType, bool) {
	switch b[0] {
	case byte('s'):
		if bytes.Equal(b[1:], sessBytes) {
			return Sess, true
		}
	case byte('r'):
		if bytes.Equal(b[1:], reqBytes) {
			return Req, true
		}
	case byte('b'):
		if bytes.Equal(b[1:], bereqBytes) {
			return BeReq, true
		}
	}
	return TxUnknown, false
}

func bytes2Reason(b []byte) (Reason, bool) {
	switch b[0] {
	case byte('H'):
		if bytes.Equal(b[1:], http1Bytes) {
			return HTTP1, true
		}
	case byte('r'):
		if bytes.Equal(b[1:], rxreqBytes) {
			return RxReq, true
		}
		if bytes.Equal(b[1:], restartBytes) {
			return Restart, true
		}
	case byte('e'):
		if bytes.Equal(b[1:], esiBytes) {
			return ESI, true
		}
	case byte('p'):
		if bytes.Equal(b[1:], passBytes) {
			return Pass, true
		}
		if bytes.Equal(b[1:], pipeBytes) {
			return Pipe, true
		}
	case byte('f'):
		if bytes.Equal(b[1:], fetchBytes) {
			return Fetch, true
		}
	case byte('b'):
		if bytes.Equal(b[1:], bgFetchBytes) {
			return BgFetch, true
		}
	}
	return ReasonUnknown, false
}

type grpNode struct {
	children []uint32
	vxid     uint32
	pvxid    uint32
	done     uint
}

// A Query provides the means to read aggregated transactions from the
// log.  A Query must be created from a Cursor using the NewQuery
// function.
type Query struct {
	cursor           *Cursor
	expr             *qExpr
	incomplete       map[uint32]Tx
	ignoreSess       map[uint32]existence
	grpNodes         map[uint32]grpNode
	IncompleteTxHigh uint
	grp              Grouping
}

func (c *Cursor) rawTxGrp() []Tx {
	rec := c.Record()
	return []Tx{{
		Type:       TxRaw,
		Reason:     ReasonUnknown,
		Level:      0,
		VXID:       uint32(rec.VXID),
		ParentVXID: 0,
		Records:    []Record{rec},
	}}
}

// NewQuery creates a Query from a Cursor. The Grouping argument indicates
// how transactions from the log should be aggregated: VXID, Request, Session
// or Raw grouping.
//
// If the query argument is non-empty, it is interpreted as a query
// string for filtering log transactions. See vsl-query(7) for details
// of the query language and its semantics. When a query is in effect,
// only transaction groups that match the query are returned by
// NextTxGroup().
//
// Queries currently do not support singly-quoted strings (only double
// quotes).
func (c *Cursor) NewQuery(grp Grouping, query string) (*Query, error) {
	var expr *qExpr
	if c == nil || c.cursor == nil {
		return nil, errors.New("Cursor is nil or uninitialized")
	}
	if err := c.log.checkNil(); err != nil {
		return nil, err
	}
	if query != "" {
		qexpr, err := parseQuery(query)
		if err != nil {
			return nil, err
		}
		expr = qexpr
	}
	txMap := make(map[uint32]Tx)
	sessSet := make(map[uint32]existence)
	grpNodes := make(map[uint32]grpNode)
	return &Query{
		cursor:           c,
		expr:             expr,
		grp:              grp,
		incomplete:       txMap,
		ignoreSess:       sessSet,
		grpNodes:         grpNodes,
		IncompleteTxHigh: 0,
	}, nil
}

// Parses a Begin or Link record for the purposes of grouping
func (q *Query) parseRec(payload Payload) (TxType, uint32, Reason, error) {
	min, max, ok := fieldNDelims(payload, 0)
	if !ok {
		// XXX error handling
		return TxUnknown, 0, ReasonUnknown, nil
	}
	txtype, ok := bytes2Type(payload[min:max])
	if !ok {
		// XXX error handling
		return TxUnknown, 0, ReasonUnknown, nil
	}
	min, max, ok = fieldNDelims(payload, 1)
	if !ok {
		// XXX error handling
		return txtype, 0, ReasonUnknown, nil
	}
	vxid, ok := atoUint32(payload[min:max])
	if !ok {
		// XXX error handling
		return txtype, 0, ReasonUnknown, nil
	}
	min, max, ok = fieldNDelims(payload, 2)
	if !ok {
		// XXX error handling
		return txtype, vxid, ReasonUnknown, nil
	}
	reason, ok := bytes2Reason(payload[min:max])
	if !ok {
		// XXX error handling
		return TxUnknown, vxid, ReasonUnknown, nil
	}
	return txtype, vxid, reason, nil
}

func (q *Query) link(pvxid uint32, vxid uint32) {
	child, cexists := q.grpNodes[vxid]
	if !cexists {
		child = grpNode{vxid: vxid}
	}
	child.pvxid = pvxid
	q.grpNodes[vxid] = child
	if pvxid == 0 {
		return
	}
	parent, pexists := q.grpNodes[pvxid]
	if !pexists {
		parent = grpNode{vxid: pvxid}
	}
	found := slices.Contains(parent.children, vxid)
	if !found {
		parent.children = append(parent.children, vxid)
	}
	q.grpNodes[pvxid] = parent
}

func (q *Query) addRec(rec Record) Tx {
	vxid32 := uint32(rec.VXID)
	incmplTx, exists := q.incomplete[vxid32]
	incmplTx.Records = append(incmplTx.Records, rec)
	q.incomplete[vxid32] = incmplTx

	// XXX handle error if the tx was not found
	_ = exists

	return incmplTx
}

func (q *Query) doBegin(rec Record) {
	vxid32 := uint32(rec.VXID)
	txtype, pvxid, reason, err := q.parseRec(rec.Payload)
	if q.grp == Request && txtype == Sess {
		// In request grouping we ignore all session records
		q.ignoreSess[vxid32] = exister
		return
	}
	// XXX handle errors: parsing Begin, sess Tx already in map
	_ = err

	tx := Tx{
		Type:       txtype,
		Reason:     reason,
		VXID:       vxid32,
		ParentVXID: pvxid,
		Level:      1,
		Records:    []Record{rec},
	}
	q.incomplete[vxid32] = tx
	if uint(len(q.incomplete))+1 > q.IncompleteTxHigh {
		q.IncompleteTxHigh = uint(len(q.incomplete)) + 1
	}

	if q.grp == VXID {
		return
	}

	if !(rec.Type == Client && q.grp == Request && reason == RxReq) {
		q.link(pvxid, vxid32)
	}
}

func (q *Query) doLink(rec Record) {
	vxid := uint32(rec.VXID)
	if q.grp == Request {
		if _, exists := q.ignoreSess[vxid]; exists {
			return
		}
	}

	q.addRec(rec)
	if q.grp == VXID {
		return
	}
	txtype, vxid, reason, err := q.parseRec(rec.Payload)
	q.link(uint32(rec.VXID), vxid)
	// XXX VSL checks: parse err, unknown types, vxid==0, link to self,
	// duplicate link, link too late, type mismatch
	_ = txtype
	_ = reason
	_ = err
}

func (q *Query) doEnd(rec Record) []Tx {
	vxid32 := uint32(rec.VXID)
	if q.grp == Request {
		if _, exists := q.ignoreSess[vxid32]; exists {
			delete(q.ignoreSess, vxid32)
			return nil
		}
	}

	tx := q.addRec(rec)
	if q.grp == VXID {
		txGrp := []Tx{tx}
		delete(q.incomplete, vxid32)
		if q.expr != nil && !q.expr.eval(txGrp) {
			return nil
		}
		return txGrp
	}

	node, exists := q.grpNodes[vxid32]
	if !exists {
		txGrp := []Tx{tx}
		if q.expr != nil && !q.expr.eval(txGrp) {
			return nil
		}
		return txGrp
	}
	q.incomplete[vxid32] = tx
	for node.pvxid != 0 && node.done == uint(len(node.children)) {
		current := node.pvxid
		node, exists = q.grpNodes[node.pvxid]
		// XXX panic
		_ = exists
		node.done++
		q.grpNodes[current] = node
	}
	if node.pvxid != 0 || node.vxid != uint32(rec.VXID) ||
		node.done < uint(len(node.children)) {
		return nil
	}
	tx, exists = q.incomplete[node.vxid]
	// XX panic
	_ = exists
	tx.Level = 1
	tx.ParentVXID = 0
	txGrp := []Tx{tx}
	delete(q.incomplete, node.vxid)
	nodeQ := node.children
	delete(q.grpNodes, node.vxid)

	// Add to the txGrp in breadth-first order
	// XXX setting tx.Level
	for len(nodeQ) > 0 {
		var nextQ []uint32
		for _, vxid := range nodeQ {
			tx, exists := q.incomplete[vxid]
			// XX panic
			_ = exists
			txGrp = append(txGrp, tx)
			delete(q.incomplete, vxid)
			node, exists = q.grpNodes[vxid]
			// XXX panic
			_ = exists
			nextQ = append(nextQ, node.children...)
			delete(q.grpNodes, vxid)
		}
		nodeQ = nextQ
	}
	if q.expr != nil && !q.expr.eval(txGrp) {
		return nil
	}
	return txGrp
}

func (q *Query) doRec(rec Record) {
	if q.grp == Request {
		if _, exists := q.ignoreSess[uint32(rec.VXID)]; exists {
			return
		}
	}
	q.addRec(rec)
}

// NextTxGroup returns the next group of transactions from the
// log. Transactions are aggregated according to the Grouping argument
// given in NewQuery(), and filtered according to the query
// expression.
//
// The Status return value indicates whether there was an error
// reading the log, whether there was an end condition (EOL or EOF),
// or if there are more transactions to be read from the log.
func (q *Query) NextTxGroup() ([]Tx, Status) {
	for {
		status := q.cursor.Next()
		if status != More {
			return nil, status
		}
		if q.grp == GRaw {
			return q.cursor.rawTxGrp(), status
		}

		backend := q.cursor.Backend()
		client := q.cursor.Client()
		if !backend && !client {
			continue
		}

		rec := q.cursor.Record()
		switch rec.Tag {
		case Tag(C.SLT_Begin):
			q.doBegin(rec)
		case Tag(C.SLT_Link):
			q.doLink(rec)
		case Tag(C.SLT_End):
			if txGrp := q.doEnd(rec); txGrp != nil {
				return txGrp, status
			}
		default:
			q.doRec(rec)
		}
	}
}
