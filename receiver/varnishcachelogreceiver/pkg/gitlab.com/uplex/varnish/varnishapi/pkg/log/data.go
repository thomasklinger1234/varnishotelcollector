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

int
slt_max()
{
	return SLT__MAX;
}

int
nonprintable(enum VSL_tag_e tag)
{
	return (VSL_tagflags[tag] & (SLT_F_BINARY | SLT_F_UNSAFE));
}
*/
import "C"

// TxType is a classifier for a log transaction, indicating if it is
// the log of a client or backend request/response, a Varnish session,
// or a raw transaction.
type TxType uint8

const (
	// TxUnknown is rarely generated from log reads.
	TxUnknown TxType = TxType(C.VSL_t_unknown)

	// Sess indicates the log of a session, which stands for the
	// "conversation" that Varnish has with a single client
	// connection, comprising one or more requests/responses.
	Sess = TxType(C.VSL_t_sess)

	// Req indicates a client request/response log.
	Req = TxType(C.VSL_t_req)

	// BeReq indicates a backend request/response log.
	BeReq = TxType(C.VSL_t_bereq)

	// TxRaw indicates a raw transaction with only one Record,
	// such as the log of a backend health check.
	TxRaw = TxType(C.VSL_t_raw)
)

// String returns the string for a TxType that appears in the "title"
// line of a log transaction in varnishlog output.
func (txtype TxType) String() string {
	switch txtype {
	case TxUnknown:
		return "Unknown"
	case Sess:
		return "Session"
	case Req:
		return "Request"
	case BeReq:
		return "BeReq"
	case TxRaw:
		return "Record"
	default:
		return "invalid transaction type"
	}
}

// Reason is a classifier for the cause of the event that was logged.
type Reason uint8

const (
	// ReasonUnknown is the reason for raw transactions (since
	// they are not grouped with the transaction for the event
	// that initiated the logged event).
	ReasonUnknown Reason = Reason(C.VSL_r_unknown)

	// HTTP1 indicates the start of an HTTP/1 session.
	HTTP1 = Reason(C.VSL_r_http_1)

	// RxReq indicates that a client request was received.
	RxReq = Reason(C.VSL_r_rxreq)

	// ESI indicates an edge side include subrequest.
	ESI = Reason(C.VSL_r_esi)

	// Restart indicates that request restart was invoked by VSL.
	Restart = Reason(C.VSL_r_restart)

	// Pass is a backend request for which cache lookup was
	// bypassed.
	Pass = Reason(C.VSL_r_pass)

	// Fetch is a synchronous backend fetch.
	Fetch = Reason(C.VSL_r_fetch)

	// BgFetch is a backend fetch in the background.
	BgFetch = Reason(C.VSL_r_bgfetch)

	// Pipe is an exchange of requests and responses between a
	// client and backend with no intervention by Varnish, until
	// one or the other side closes the connection.
	Pipe = Reason(C.VSL_r_pipe)
)

// String returns the string for a Reason that appears as the third
// field of a "Begin" record in varnishlog output (except for raw
// transactions).
func (reason Reason) String() string {
	switch reason {
	case ReasonUnknown:
		return "unknown"
	case HTTP1:
		return "HTTP/1"
	case RxReq:
		return "rxreq"
	case ESI:
		return "esi"
	case Restart:
		return "restart"
	case Pass:
		return "pass"
	case Fetch:
		return "fetch"
	case BgFetch:
		return "bgfetch"
	case Pipe:
		return "pipe"
	default:
		return "invalid reason"
	}
}

// Grouping mode determines how transactions are aggregated when read
// from the log. Depending on the mode, transactions in a group may
// form a hierarchy. The default Grouping is VXID.
type Grouping uint8

const (
	// GRaw for raw grouping. Transactions are not grouped, nor
	// are records grouped into transactions -- every transaction
	// has exactly one record. Records unrelated to requests,
	// responses and sessions, such as backend health checks, are
	// only reported in this mode.
	GRaw Grouping = Grouping(C.VSL_g_raw)

	// VXID is the default mode. Transactions are not grouped --
	// each read returns one transaction for a request/response
	// (client or backend) or session. Non-transactional data,
	// such as backend health checks, are not reported in this
	// mode. VXID is the unique ID generated by Varnish for each
	// transaction.
	VXID = Grouping(C.VSL_g_vxid)

	// Request indicates grouping by client request. A group
	// contains the client request and any other transactions that
	// it initiated, such as backend requests, ESI subrequests and
	// restarts,
	Request = Grouping(C.VSL_g_request)

	// Session grouping aggregates all transactions over a client
	// connection. These groups can be much larger than the
	// others; they may occupy significantly more memory, and log
	// reads with this mode may take much longer.
	Session = Grouping(C.VSL_g_session)
)

// Str2Grp maps strings to Grouping constants.
var Str2Grp = map[string]Grouping{
	"raw":     GRaw,
	"vxid":    VXID,
	"request": Request,
	"session": Session,
}

// RecordType is a classifier for the data in a Record, indicating if
// it is part of the log for a client request/response, a backend
// request/repsonse, or neither.  Corresponds to the indicator 'b',
// 'c' or '-' in the fourth column of verbose varnishlog output.
//
// Use rune(rt) for a RecordType rt to get 'b', 'c' or '-'
type RecordType uint8

const (
	// Client Record -- part of the log for a client
	// request/response.
	Client RecordType = RecordType('c')

	// Backend Record -- part of the log for a backend
	// request/response.
	Backend = RecordType('b')

	// None is the type of a Record that is not part of the log of
	// a backend or client transaction. Examples are records with
	// tags such as "Backend_health", "CLI" and so forth.
	None = RecordType('-')
)

// String returns "b", "c" or "-".
func (rt RecordType) String() string {
	switch rt {
	case Client:
		return "c"
	case Backend:
		return "b"
	case None:
		return "-"
	default:
		return "invalid RecordType"
	}
}

// TagData contains metadata about log tags, particularly their string
// form, since a tag appears as a number in a Record. The type of the
// Tags variable is TagData[]; for a tag t, use TagData[t] to get its
// metadata.
//
// String is the Tag name that appears in the second column of default
// varnishlog output, such as "ReqHeader" for a client request header,
// or "BerespHeader" for a backend response header.
//
// If Tags[t].Legal is false, then there is no legal tag for t. No
// such tag is generated from log reads for a live instance of
// Varnish. You may encounter such a tag when reading from a binary
// log that was generated by a version of Varnish that is different
// from the version with which the Go client is currently linked.
//
// NonPrintable is true if log payloads of the type indicated by the
// tag may contain non-printable characters. If NonPrintable is false
// (which is the case for most tags), you can assume that the log
// payload contains only printable ASCII characters.
type TagData struct {
	String       string
	Legal        bool
	NonPrintable bool
}

func initTags() []TagData {
	var tags []TagData
	if uint8(C.slt_max()) > ^uint8(0) {
		panic("SLT__MAX > max uint8")
	}
	max := int(C.slt_max())
	for i := 0; i < max; i++ {
		tagdata := TagData{}
		if C.VSL_tags[i] != nil {
			tagdata.String = C.GoString(C.VSL_tags[i])
			tagdata.Legal = true
			tagdata.NonPrintable = C.nonprintable(uint32(i)) != 0
		}
		tags = append(tags, tagdata)
	}
	return tags
}

// Tags has the type []TagData -- for a tag t in a Record, Tags[t]
// contains its metadata.
var Tags = initTags()

// A Tag is a classifier for the contents of a Record's payload,
// corresponding to the second column of default varnishlog
// output. For a Tag t, Tags[t].String may be "ReqURL" for a client
// request URL, "BerespStatus" for the HTTP status of a backend
// response, and so forth.
type Tag uint8

// String returns the string form for a tag (the value in
// Tags[tag].String). This makes it easy to get the string form with
// the %s or %v verbs for a formatter in package fmt.
func (tag Tag) String() string {
	return Tags[tag].String
}

// Payload is the type of the message contained in a Record -- the
// specific data for the record.
//
// A Payload is just a byte slice, so it is easily converted to a
// string (and otherwise manipulated).
type Payload []byte

// String returns a payload as a string.
func (p Payload) String() string {
	return string(p)
}

// A Record in the Varnish log, corresponding to a line of output from
// the varnishlog utility. Instances of this type are returned for log
// reads, possibly aggregated into transactions (the Tx type), and
// form the bulk of log data.
//
// The contents and format of log records are described in vsl(7):
//
// https://varnish-cache.org/docs/trunk/reference/vsl.html
type Record struct {
	// Indicates whether this record is part of a client or
	// backend log, or neither.
	Type RecordType

	// A classifier for the contents of this Record's payload. Use
	// Tag.String() to get tag names such as "ReqURL" or
	// "BerespHeader" that appear in the second column of default
	// varnishlog output.
	Tag Tag

	// The unique ID generated by Varnish for this log
	// transaction.
	VXID uint

	// The log payload is the specific data for this record. The
	// payload appears after the second column of default
	// varnishlog output.
	//
	// If Nonprintable() is true for the tag in this record, then
	// the payload may contain nonprintable characters. Otherwise
	// it contains only printable ASCII characters (which is the
	// case for most records).
	Payload Payload
}

// A Tx for "transaction" is a collection of related log records,
// typically as the log of a client or backend request and
// response. Transactions may also include data unrelated to requests
// and responses, such as backend health checks.
//
// Transactions are read from the log in groups, according to the
// Grouping selected for the log client, corresponding to the grouping
// modes set by varnishlog -g. Transactions in a group may form a
// hierarchy, in which case they are related by the parent VXID
// property -- a transaction's parent VXID is the VXID of the
// transaction that initiated it. Parent VXID 0 indicates that a
// transaction has no parent transaction.
//
// The depth of nesting in a hierarchy is represented by a
// transaction's Level property. Levels start at level 1, except for
// raw transactions, which always have level 0.
type Tx struct {
	// Whether this is the log of a client request/response,
	// backend request/response, session and so forth.
	Type TxType

	// The cause of the event logged by this transaction.
	Reason Reason

	// The level of this transaction in a group. Always 1 for VXID
	// grouping, and always 0 for raw grouping.
	Level uint

	// The unique ID generated by Varnish for this transaction.
	VXID uint32

	// The ID of the parent transaction for this transaction in a
	// group. 0 if there is no parent. Always 0 for VXID and raw
	// groupings.
	ParentVXID uint32

	// The Records that make up the bulk of this transaction's
	// data.
	Records []Record
}

// Status classifies the current state of a log read sequence. A value
// of this type is returned from the Cursor.Next() and
// Query.NextTxGroup() methods.
type Status int8

const (
	// WriteErr while writing a log.
	WriteErr Status = Status(C.vsl_e_write)

	// IOErr while reading a log.
	IOErr = Status(C.vsl_e_io)

	// Overrun indicates that the write position of a live Varnish
	// instance, writing into the ring buffer, has cycled around
	// and overtaken the read position of the Log client.
	Overrun = Status(C.vsl_e_overrun)

	// Abandoned indicates tha the log was closed or abandoned
	// (usually because the Varnish child process stopped).
	Abandoned = Status(C.vsl_e_abandon)

	// EOF indicates that the end of a binary log file was reached.
	EOF = Status(C.vsl_e_eof)

	// EOL indicates that the end of a live Varnish log buffer was
	// reached (there are currently no new log data).
	EOL = Status(C.vsl_end)

	// More indicates that there are more records to be read from
	// the log.
	More = Status(C.vsl_more)
)

// String returns a string describing the Status.
func (status Status) String() string {
	switch status {
	case WriteErr:
		return "write error"
	case IOErr:
		return "I/O read error"
	case Overrun:
		return "Log overrun"
	case Abandoned:
		return "Log was abandoned or closed"
	case EOF:
		return "End of file"
	case EOL:
		return "End of log"
	case More:
		return "More log transactions are pending"
	default:
		return "invalid status"
	}
}

// Error returns the same string as String for a Status, so that
// Status implements the error interface.
//
// Some of the strings returned, such as for EOF or EOL, do not
// necessarily describe an error.
func (status Status) Error() string {
	return status.String()
}
