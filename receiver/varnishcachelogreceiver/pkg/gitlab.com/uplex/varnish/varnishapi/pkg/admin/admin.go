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

// Package admin implements a client for the Varnish administrative
// interface, or CLI. An Admin client establishes a network connection
// with the port at which the Varnish management process accepts
// administrative commands, and implements the protocol documented in
// varnish-cli(7). The commands in the CLI should be familiar to users
// of varnishadm(1).
//
// The Command() method can be used to issue arbitrary commands. Some
// of the methods encapsulate CLI commands and parse the output,
// returning information as structured data.
//
// The CLI implements an authentication protocol that depends on a
// shared secret (see varnish-cli(7)). The secret data are in a file
// that is either set by the varnishd -S option, or generated
// automatically when Varnish starts (see varnishd(1)).
//
// Methods in this package that establish the CLI connection execute
// the authentication transparently, and require the secret in byte
// slice parameters.  This data can be read from the file (for example
// with ioutil.ReadAll()), or client code can generate the secret and
// write the file, and then start Varnish with the -S option pointing
// to the file. Varnish reads the file anew when CLI authentication is
// necessary, so the file's contents can be changed at runtime.
//
// Clients that are running on the same host as the Varnish instance,
// and have sufficient permissions to attach to Varnish shared memory
// and read the secret file, can use the GetMgmtAddr() and
// GetSecretPath() of the vsm package to read the parameters from Varnish
// shared memory. Initiating a client connection could look like this:
//
//	import "gitlab.com/uplex/varnish/varnishapi/pkg/admin"
//	import "gitlab.com/uplex/varnish/varnishapi/pkg/vsm"
//
//	// Initialize shared memory attachment
//	vsm := vsm.New()
//	if vsm == nil {
//		// error handling
//	}
//	defer vsm.Destroy()
//
//	// Use "" for the default instance, or use the instance name
//	if err := vsm.Attach(""); err != nil {
//		// error handling
//	}
//
//	// Get the management address
//	address, err := vsm.GetMgmtAddr()
//	if err != nil {
//		// error handling
//	}
//
//	// Get the path of the secret file
//	spath, err := vsm.GetSecretPath()
//	if err != nil {
//		// error handling
//	}
//	// Get the contents of the secret file
//	secret, err = ioutil.ReadAll(sfile)
//	if err != nil {
//		// error handling
//	}
//
//	// Dial connects to the management address, executes the
//	// authentication protocol on successful connection, and
//	// returns an Admin object.
//	adm, err := Dial(address, secret, time.Duration(0))
//	if err != nil {
//		// error handling
//	}
//	defer adm.Close()
//
//	// The adm object can now be used to issue CLI commands to
//	// the Varnish management process using the methods in this
//	// package.
package admin

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	network   = "tcp"
	statusLen = 13
)

// A ResponseCode is an IETF-style 3-digit code that classifies the
// handling of an admin command.
type ResponseCode uint

const (
	// Syntax error
	Syntax ResponseCode = ResponseCode(100)

	// Unknown command
	Unknown = ResponseCode(101)

	// Unimpl indicates that the command is reserved for a future
	// implementation.
	Unimpl = ResponseCode(102)

	// TooFew indicates that there were not enough arguments for
	// the command.
	TooFew = ResponseCode(104)

	// TooMany indicates that there too many arguments for the
	// command.
	TooMany = ResponseCode(105)

	// Param indicates that there was an invalid command
	// argument. This is the response for example when a VCL load
	// fails due to a compile error, or when information about a
	// runtime parameter was requested, but the parameter name
	// does not exist.
	Param = ResponseCode(106)

	// Auth indicates that authentication is required to use the
	// CLI. This should be handled transparently by the code in
	// this package.
	Auth = ResponseCode(107)

	// OK indicates that the command was executed succesfully.
	OK = ResponseCode(200)

	// Truncated indicates that the command was executed
	// succesfully, but the response text (in the Msg field of
	// Response) was truncated.
	Truncated = ResponseCode(201)

	// Cant indicates that the command could not be executed, for
	// example when the "start" command is issued, but the child
	// process is already running.
	Cant = ResponseCode(300)

	// Comms indicates an error at the network or protocol level.
	Comms = ResponseCode(400)

	// Close indicates the Varnish is closing the admin
	// connection.
	Close = ResponseCode(500)
)

func (rc ResponseCode) String() string {
	switch rc {
	case Syntax:
		return "Syntax error"
	case Unknown:
		return "Unknown command"
	case Unimpl:
		return "Command not implemented"
	case TooFew:
		return "Not enough arguments for command"
	case TooMany:
		return "Too many arguments for command"
	case Param:
		return "Illegal parameters"
	case Auth:
		return "Authentication required"
	case OK:
		return "OK"
	case Truncated:
		return "Response was truncated"
	case Cant:
		return "Unable to execute command"
	case Comms:
		return "Communication/protocol error"
	case Close:
		return "Closing connection"
	default:
		panic("Invalid response code")
	}
}

// A Response is the response from Varnish to a command.
type Response struct {
	Msg  string
	Code ResponseCode
}

// An UnexpectedResponse wraps a Response that indicates that a
// command produced an error. It implements the error interface.
type UnexpectedResponse struct {
	Response Response
}

func (badResp UnexpectedResponse) Error() string {
	return fmt.Sprintf("unexpected response %d (%s): %s",
		badResp.Response.Code, badResp.Response.Code,
		badResp.Response.Msg)
}

// An Admin is a client for the Vanrish administrative interface.
type Admin struct {
	conn net.Conn
	lsnr net.Listener
	// The Banner text returned from Varnish when a CLI connection
	// is established. This containes information about the
	// Varnish version and some of its startup parameters.
	Banner string
}

func read(conn net.Conn) (Response, error) {
	resp := Response{}
	if conn == nil {
		return resp, errors.New("not connected")
	}
	bytes := make([]byte, statusLen)
	if _, err := conn.Read(bytes); err != nil {
		return resp, err
	}
	line := strings.TrimSpace(string(bytes))
	status := strings.Split(line, " ")
	code, err := strconv.Atoi(status[0])
	if err != nil {
		return resp, err
	}
	length, err := strconv.Atoi(status[1])
	if err != nil {
		return resp, err
	}
	bytes = make([]byte, length+1)
	if _, err = conn.Read(bytes); err != nil {
		return resp, err
	}
	resp.Msg = strings.TrimSpace(string(bytes))
	resp.Code = ResponseCode(code)
	return resp, nil
}

func auth(conn net.Conn, secret []byte) (Response, error) {
	resp, err := read(conn)
	if err != nil {
		return resp, err
	}
	if resp.Code != Auth {
		return resp, nil
	}
	challenge := resp.Msg[:strings.IndexByte(resp.Msg, '\n')+1]
	h := sha256.New()
	h.Write([]byte(challenge))
	h.Write(secret)
	h.Write([]byte(challenge))
	sum := h.Sum([]byte(nil))
	answer := fmt.Sprintf("auth %0x\n", sum)
	if _, err := conn.Write([]byte(answer)); err != nil {
		return resp, err
	}
	resp, err = read(conn)
	if err != nil {
		return resp, err
	}
	if resp.Code != OK {
		return resp, errors.New("failed authentication")
	}
	return resp, nil
}

func (adm *Admin) checkNil() error {
	if adm == nil {
		return errors.New("Admin object is nil")
	}
	return nil
}

func (adm *Admin) checkConn() error {
	if err := adm.checkNil(); err != nil {
		return err
	}
	if adm.conn == nil {
		return errors.New("Connection is uninitialized")
	}
	return nil
}

// Dial connects to the administrative port of a running Varnish
// management process (which may have been set by the varnishd -T
// option).
//
// addr is a string representing the address and port to which to
// connect, exactly as documented for net.Dial (the network class is
// implicitly "tcp"). timeout establishes a timeout for the
// connection, exactly as documented for net.DialTimeout.
//
// secret must contain the contents of the shared secret file, as
// documented above. Dial() implements the authentication
// transparently, and returns a non-nil error if it fails.
func Dial(addr string, secret []byte, timeout time.Duration) (*Admin, error) {
	conn, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		return nil, err
	}
	resp, err := auth(conn, secret)
	if err != nil {
		return nil, err
	}
	if resp.Code != OK {
		return nil, UnexpectedResponse{Response: resp}
	}
	return &Admin{
		conn:   conn,
		Banner: resp.Msg,
	}, nil
}

// Listen initializes a local address at which the client will listen
// for a connection from the Varnish management process, over which
// Varnish will offer the CLI. This can be used when varnishd is
// started with the -M option (see varnishd(1)).
//
// addr represents the address at which to listen, exactly as
// documented for net.Listen (the network class is implicitly "tcp").
func Listen(addr string) (*Admin, error) {
	lsnr, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	return &Admin{
		lsnr: lsnr,
	}, nil
}

// Accept accepts a connection from the Varnish management process,
// when the Admin instance was initialized with Listen() and Varnish
// was invoked with the -M option.
//
// secret must contain the contents of the shared secret file, as
// documented above. Accept() implements the authentication
// transparently, and returns a non-nil error if it fails.
//
// As with net.Accept, Accept() blocks until a connection has been
// accepted.
func (adm *Admin) Accept(secret []byte) error {
	if err := adm.checkNil(); err != nil {
		return err
	}
	if adm.lsnr == nil {
		return errors.New("Listener is uninitialized")
	}
	conn, err := adm.lsnr.Accept()
	if err != nil {
		return err
	}
	resp, err := auth(conn, secret)
	if err != nil {
		return err
	}
	adm.conn = conn
	adm.Banner = resp.Msg
	return nil
}

// Close the admin connection (wisely used with defer after a
// connection has been established).
func (adm *Admin) Close() error {
	if err := adm.checkNil(); err != nil {
		return err
	}
	if adm.lsnr != nil {
		if err := adm.lsnr.Close(); err != nil {
			return err
		}
	}
	if adm.conn != nil {
		if err := adm.conn.Close(); err != nil {
			return err
		}
	}
	return nil
}

// LocalAddr returns a net.Addr representing the local address of an
// admin connection, if it was established with Dial() (see
// net.Conn.LocalAddr).
func (adm *Admin) LocalAddr() (net.Addr, error) {
	if err := adm.checkConn(); err != nil {
		return nil, err
	}
	return adm.conn.LocalAddr(), nil
}

// RemoteAddr returns a net.Addr representing the remote address of an
// admin connection (the address at which the Varnish management
// process is listening), if it was established with Dial() (see
// net.Conn.RemoteAddr).
func (adm *Admin) RemoteAddr() (net.Addr, error) {
	if err := adm.checkConn(); err != nil {
		return nil, err
	}
	return adm.conn.RemoteAddr(), nil
}

// ListenerAddr returns a net.Addr representing the address at which
// the client is listening for an admin connection, if it was
// initialized with Listen() (see net.Listener.Addr).
//
// This makes it possible, for example, to invoke Listen() with port 0
// for "any available port", then retrieve the address with the
// selected port, and start Varnish with the -M option to connect to
// that port.
func (adm *Admin) ListenerAddr() (net.Addr, error) {
	if err := adm.checkNil(); err != nil {
		return nil, err
	}
	if adm.lsnr == nil {
		return nil, errors.New("Listener is uninitialized")
	}
	return adm.lsnr.Addr(), nil
}

// SetDeadline establishes read and write deadlines for the admin
// connection.  See net.Conn.SetDeadline().
func (adm *Admin) SetDeadline(t time.Time) error {
	if err := adm.checkConn(); err != nil {
		return err
	}
	if err := adm.conn.SetDeadline(t); err != nil {
		return err
	}
	return nil
}

// SetReadDeadline sets the deadline for reads on the admin
// connection.  See net.Conn.SetReadDeadline().
func (adm *Admin) SetReadDeadline(t time.Time) error {
	if err := adm.checkConn(); err != nil {
		return err
	}
	if err := adm.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return nil
}

// SetWriteDeadline sets the deadline for writes on the admin
// connection.  See net.Conn.SetWriteDeadline().
func (adm *Admin) SetWriteDeadline(t time.Time) error {
	if err := adm.checkConn(); err != nil {
		return err
	}
	if err := adm.conn.SetWriteDeadline(t); err != nil {
		return err
	}
	return nil
}

// Command issues arbitrary commands to the admin interface, as
// documented in varnish-cli(7):
//
//	resp, err := adm.Command("status")
//	resp, err := adm.Command("vcl.load", "myconfig", "/path/to/my.vcl")
//
// Returns a non-nil error only if there was an error at the network
// level. If Varnish was able to handle the command at all
// (successfully or not), then the error is nil and the response from
// Varnish is encapsulated in the Response object, which then can be
// inspected to see if the command succeeded.
func (adm *Admin) Command(cmd ...string) (Response, error) {
	resp := Response{}
	if err := adm.checkConn(); err != nil {
		return resp, err
	}
	cmd[len(cmd)-1] += "\n"
	bytes := []byte(strings.Join(cmd, " "))
	if _, err := adm.conn.Write(bytes); err != nil {
		return resp, err
	}
	resp, err := read(adm.conn)
	if err != nil {
		return resp, err
	}
	return resp, nil
}
