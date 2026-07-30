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

	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	notimeout = time.Duration(-1 * time.Second)
)

// fields implements the flag.Value interface, to enable mutliple -f
// flags.
type fields []string

func (flds *fields) String() string {
	return fmt.Sprintf("%s", *flds)
}

func (flds *fields) Set(value string) error {
	*flds = append(*flds, value)
	return nil
}

var (
	inst = flag.String("n", "", "varnish instance name")
	tmo  = flag.Duration("t", notimeout, "VSM connection timeout")
	once = flag.Bool("1", false, "Print the statistics to stdout")
	list = flag.Bool("l", false,
		"Lists the available fields to use with the -f option")
	jsn  = flag.Bool("j", false, "Print statistics to stdout as JSON")
	xmlf = flag.Bool("x", false, "Print statistics to stdout as XML")
)

func errExit(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func doList(vstats *stats.Stats) {
	names, err := vstats.Names()
	if err != nil {
		errExit(err)
	}

	fmt.Println("govarnishstat -f option fields:")
	fmt.Println("Field name                     Description")
	fmt.Println("----------                     -----------")
	for _, name := range names {
		desc, err := vstats.D9n(name)
		if err != nil {
			errExit(err)
		}
		fmt.Print(name)
		fmt.Printf("%*s %s\n", len(name)-30, "", desc)
	}
	os.Exit(0)
}

func doOnce(vstats *stats.Stats) {
	var uptime uint64
	pad := 18

	uptimeHndlr := func(name string, val uint64) bool {
		if name == "MAIN.uptime" {
			uptime = val
			return false
		}
		return true
	}
	if err := vstats.Read(uptimeHndlr); err != nil {
		errExit(err)
	}

	onceHndlr := func(name string, val uint64) bool {
		i := len(name)
		desc, err := vstats.D9n(name)
		if err != nil {
			errExit(err)
		}

		fmt.Print(name)
		if i > pad {
			pad = i + 1
		}
		fmt.Printf("%*.*s%12v ", pad-i, pad-i, "", val)
		if desc.Semantics == stats.Counter {
			avg := float64(val)
			if uptime != 0 {
				avg /= float64(uptime)
			}
			fmt.Printf("%12.2f ", avg)
		} else {
			fmt.Printf("%12s ", ".  ")
		}
		fmt.Println(desc)
		return true
	}
	if err := vstats.Read(onceHndlr); err != nil {
		errExit(err)
	}
	os.Exit(0)
}

type jsonStats map[string]interface{}
type jsonStat struct {
	Description string `json:"description"`
	Flag        string `json:"flag"`
	Format      string `json:"format"`
	Value       uint64 `json:"value"`
}

func doJSON(vstats *stats.Stats) {
	jstats := jsonStats{}

	rdHndlr := func(name string, val uint64) bool {
		jstat := jsonStat{}
		d9n, err := vstats.D9n(name)
		if err != nil {
			errExit(nil)
		}
		jstat.Description = d9n.ShortD9n
		jstat.Flag = d9n.Semantics.String()
		jstat.Format = d9n.Format.String()
		jstat.Value = val
		jstats[name] = jstat
		return true
	}

	jstats["timestamp"] = time.Now().Format(time.RFC3339)
	if err := vstats.Read(rdHndlr); err != nil {
		errExit(err)
	}

	enc, err := json.MarshalIndent(jstats, "", "  ")
	if err != nil {
		errExit(err)
	}
	fmt.Println(string(enc))
	os.Exit(0)
}

type xmlStat struct {
	Name        string `xml:"name"`
	Value       uint64 `xml:"value"`
	Flag        string `xml:"flag"`
	Format      string `xml:"format"`
	Description string `xml:"description"`
}

type varnishstat struct {
	Timestamp string    `xml:"timestamp,attr"`
	Stat      []xmlStat `xml:"stat"`
}

func doXML(vstats *stats.Stats) {
	xstats := varnishstat{}

	rdHndlr := func(name string, val uint64) bool {
		xstat := xmlStat{}
		d9n, err := vstats.D9n(name)
		if err != nil {
			errExit(nil)
		}
		xstat.Name = name
		xstat.Value = val
		xstat.Flag = d9n.Semantics.String()
		xstat.Format = d9n.Format.String()
		xstat.Description = d9n.ShortD9n
		xstats.Stat = append(xstats.Stat, xstat)
		return true
	}

	xstats.Timestamp = time.Now().Format(time.RFC3339)
	if err := vstats.Read(rdHndlr); err != nil {
		errExit(err)
	}

	enc, err := xml.MarshalIndent(xstats, "", "\t")
	if err != nil {
		errExit(err)
	}
	fmt.Println(string(enc))
	os.Exit(0)
}

func main() {
	var inExFlds fields

	flag.Var(&inExFlds, "f", "Field inclusion glob")
	flag.Parse()

	vstats := stats.New()
	if vstats == nil {
		errExit(errors.New("Could not create stats client"))
	}
	defer vstats.Release()

	for _, f := range inExFlds {
		if strings.HasPrefix(f, "^") {
			f = strings.TrimPrefix(f, "^")
			if err := vstats.Exclude(f); err != nil {
				errExit(err)
			}
			continue
		}
		if err := vstats.Include(f); err != nil {
			errExit(err)
		}
	}

	if *tmo != notimeout {
		if err := vstats.Timeout(*tmo); err != nil {
			errExit(err)
		}
	}
	if err := vstats.Attach(*inst); err != nil {
		errExit(err)
	}

	if *list {
		doList(vstats)
	}
	if *once {
		doOnce(vstats)
	}
	if *jsn {
		doJSON(vstats)
	}
	if *xmlf {
		doXML(vstats)
	}

	doWebSocks(vstats)
}
