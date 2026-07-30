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

package main

import (
	"gitlab.com/uplex/varnish/varnishapi/pkg/stats"
	"golang.org/x/net/websocket"

	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

var vstats *stats.Stats

func tableHandler(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(table))
}

type jsonD9n struct {
	Name   string `json:"name"`
	Short  string `json:"short"`
	Long   string `json:"long"`
	Level  string `json:"level"`
	Format string `json:"format"`
	S7s    string `json:"semantics"`
}

func d9nsHandler(ws *websocket.Conn) {
	names, err := vstats.Names()
	if err != nil {
		errExit(err)
	}
	var data []jsonD9n
	for _, name := range names {
		d9n, err := vstats.D9n(name)
		if err != nil {
			errExit(err)
		}
		jd9n := jsonD9n{}
		jd9n.Name = name
		jd9n.Short = d9n.ShortD9n
		jd9n.Long = d9n.LongD9n
		jd9n.Level = d9n.Level.Label
		jd9n.Format = d9n.Format.String()
		jd9n.S7s = d9n.Semantics.String()
		data = append(data, jd9n)
	}
	websocket.JSON.Send(ws, data)
}

type movAvg struct {
	n   uint
	max uint
	acc float64
}

func (avg *movAvg) update(val float64) {
	if avg.n < avg.max {
		avg.n++
	}
	avg.acc += (val - avg.acc) / float64(avg.n)
}

type state struct {
	last    uint64
	tlast   time.Time
	avg10   movAvg
	avg100  movAvg
	avg1000 movAvg
	s7s     stats.Semantics
}

var statState map[string]*state

type jStats struct {
	Name    string  `json:"name"`
	Value   uint64  `json:"value"`
	Change  float64 `json:"change"`
	Avg     float64 `json:"avg"`
	Avg10   float64 `json:"avg10"`
	Avg100  float64 `json:"avg100"`
	Avg1000 float64 `json:"avg1000"`
	Bitmap  string  `json:"bitmap"`
}

func initCB(name string, val uint64) bool {
	newState := state{}
	newState.tlast = time.Now()
	newState.last = val
	d9n, err := vstats.D9n(name)
	if err != nil {
		errExit(err)
	}
	s7s := d9n.Semantics
	newState.s7s = s7s
	if s7s == stats.Counter || s7s == stats.Gauge {
		avg10 := movAvg{0, 10, 0}
		newState.avg10 = avg10

		avg100 := movAvg{0, 100, 0}
		newState.avg100 = avg100

		avg1000 := movAvg{0, 1000, 0}
		newState.avg1000 = avg1000
	}
	statState[name] = &newState
	return true
}

func statsHandler(ws *websocket.Conn) {
	var uptime uint64

	uptimeCB := func(name string, val uint64) bool {
		if name == "MAIN.uptime" {
			uptime = val
			return false
		}
		return true
	}

	readCB := func(name string, val uint64) bool {
		now := time.Now()
		var jstats jStats
		state, ok := statState[name]
		if !ok {
			errExit(errors.New("uninitialized stat: " + name))
		}

		jstats.Name = name
		jstats.Value = val
		dsecs := now.Sub(state.tlast).Seconds()
		change := float64(val-state.last) / dsecs
		state.last = val
		state.tlast = now
		jstats.Change = change
		if state.s7s == stats.Gauge {
			state.avg10.update(float64(val))
			state.avg100.update(float64(val))
			state.avg1000.update(float64(val))
			jstats.Avg10 = state.avg10.acc
			jstats.Avg100 = state.avg100.acc
			jstats.Avg1000 = state.avg1000.acc
		} else if state.s7s == stats.Counter {
			jstats.Avg = float64(val) / float64(uptime)
			state.avg10.update(change)
			state.avg100.update(change)
			state.avg1000.update(change)
			jstats.Avg10 = state.avg10.acc
			jstats.Avg100 = state.avg100.acc
			jstats.Avg1000 = state.avg1000.acc
		} else if state.s7s == stats.S7sBitmap {
			jstats.Bitmap = fmt.Sprintf("%10.10x",
				((val >> 24) & 0xffffffffff))
		}
		websocket.JSON.Send(ws, jstats)
		return true
	}

	for {
		err := vstats.Read(uptimeCB)
		if err != nil {
			errExit(err)
		}
		err = vstats.Read(readCB)
		if err != nil {
			errExit(err)
		}
		time.Sleep(time.Second)
	}
}

var lsnr net.Listener

func ackListener(ws *websocket.Conn) {
	for {
		var msg []byte
		websocket.Message.Receive(ws, &msg)
		if len(msg) == 0 {
			fmt.Println("Client closed the connection")
			lsnr.Close()
			return
		}
	}
}

type hrStats struct {
	Avg10   float64 `json:"avg10"`
	Avg100  float64 `json:"avg100"`
	Avg1000 float64 `json:"avg1000"`
}

func hitrateHandler(ws *websocket.Conn) {
	var hits uint64
	var misses uint64
	var lastHits uint64
	var lastMisses uint64
	var gotHits bool
	var gotMisses bool

	hr10 := movAvg{0, 10, 0}
	hr100 := movAvg{0, 100, 0}
	hr1000 := movAvg{0, 1000, 0}

	hrCB := func(name string, val uint64) bool {
		if name == "MAIN.cache_hit" {
			hits = val
			gotHits = true
		} else if name == "MAIN.cache_miss" {
			misses = val
			gotMisses = true
		}
		return !(gotHits && gotMisses)
	}

	go ackListener(ws)

	for {
		var hr float64
		var dhits float64
		var dmisses float64

		gotHits = false
		gotMisses = false
		if err := vstats.Read(hrCB); err != nil {
			errExit(err)
		}
		dhits = float64(hits - lastHits)
		dmisses = float64(misses - lastMisses)
		lastHits = hits
		lastMisses = misses

		if dhits+dmisses != 0 {
			hr = dhits / (dhits + dmisses)
		} else {
			hr = 0
		}
		hr10.update(hr)
		hr100.update(hr)
		hr1000.update(hr)

		hrstats := hrStats{}
		hrstats.Avg10 = hr10.acc
		hrstats.Avg100 = hr100.acc
		hrstats.Avg1000 = hr1000.acc

		websocket.JSON.Send(ws, hrstats)
		time.Sleep(time.Second)
	}
}

func openbrowser(url string) {
	switch runtime.GOOS {
	case "freebsd":
	case "linux":
		if err := exec.Command("xdg-open", url).Start(); err != nil {
			errExit(err)
		}
	case "darwin":
		if err := exec.Command("open", url).Start(); err != nil {
			errExit(err)
		}
	}
}

func doWebSocks(s *stats.Stats) {
	vstats = s

	names, err := s.Names()
	if err != nil {
		errExit(err)
	}
	len := len(names)
	statState = make(map[string]*state, len)

	if err := s.Read(initCB); err != nil {
		errExit(err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		errExit(err)
	}
	lsnr = listener

	http.HandleFunc("/", tableHandler)
	http.Handle("/d9ns", websocket.Handler(d9nsHandler))
	http.Handle("/stats", websocket.Handler(statsHandler))
	http.Handle("/hitrate", websocket.Handler(hitrateHandler))

	url := "http://" + listener.Addr().String() + "/"
	openbrowser(url)
	fmt.Println("Listening at", url)

	http.Serve(listener, nil)
}
