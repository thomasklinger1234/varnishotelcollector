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

// testbackend implements a simple HTTP server, intended for testing
// as a backend for Varnish.
//
// When the server is started, it prints a URL to standard output
// indicating the address at which it is listening. The server does
// not use TLS.
//
// The server responds to URL paths as follows:
//
//   - /
//     200 OK
//
//   - /health
//     If the request method is PATCH, toggle the health state of
//     the server. The response is 204 No Content. By default, the
//     server is healthy.
//     For any other method, return 204 No Content if the server is
//     is healthy, otherwise 503 Service Unavailable.
//
//   - /esi
//     200 OK with a response body that contains esi:include directives
//     for the paths /include1 and /include2
//
//   - /include1 and /include2
//     200 OK with response bodies suitable for ESI includes.
//
//   - /uncacheable
//     200 OK with the Cache-Control response header set to max-age=0.
//
// For any other URL, the response is 404 Not Found.
//
// The server runs until the process is terminated with a TERM or INT
// signal.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
)

var healthy = true

// ServeHTTP implements HandlerFunc for httptest.NewServer()
func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "testbackend.go")
	switch r.URL.EscapedPath() {
	case "/":
		fmt.Fprintln(w, "<html><body>Hello World!</body></html>")
	case "/health":
		if r.Method == http.MethodPatch {
			healthy = !healthy
			w.WriteHeader(http.StatusNoContent)
			break
		}
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "<html><body>I'm sick</body></html>")
			break
		}
		w.WriteHeader(http.StatusNoContent)
	case "/esi":
		fmt.Fprintln(w, `<html><body>
BEFORE INCLUDE1
<esi:include src="/include1">
AFTER INCLUDE1

BEFORE INCLUDE2
<esi:include src="/include2">
AFTER INCLUDE2
</html></body>`)
	case "/include1":
		fmt.Fprint(w, "INCLUDE1")
	case "/include2":
		fmt.Fprint(w, "INCLUDE2")
	case "/uncacheable":
		w.Header().Set("Cache-Control", "max-age=0")
		fmt.Fprintln(w, "<html><body>Don't cache me</body></html>")
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, "Huh?")
	}
}

func main() {
	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	server := httptest.NewServer(http.HandlerFunc(ServeHTTP))
	defer server.Close()
	fmt.Println(server.URL)

	<-sigs
}
