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

// govarnishlog re-implements the varnishlog(1) tool, to demonstrate
// the varnishapi log package.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"gitlab.com/uplex/varnish/varnishapi/pkg/log"
)

const (
	maxIdle = 10 * time.Millisecond
	minIdle = time.Microsecond
	// Mask all but the 10 LSBs, changes every 1024, to update idle time.
	bitmask = uint64(0xfffffffffffffc00)
)

var (
	grpf = flag.String("g", "vxid",
		"<session|request|vxid|raw> Grouping mode")
	verbose = flag.Bool("v", false, "Verbose record printing")
	query   = flag.String("q", "", "VSL query")

	cpuprof = flag.String("cpuprofile", "", "write cpu profile to 'file'")
	memprof = flag.String("memprofile", "", "write heap profile to 'file'")
	stats   = flag.Bool("stats", false,
		"print stats about log reads to stderr before exit")

	out          = bufio.NewWriter(os.Stdout)
	totIdle      = time.Duration(0)
	tRdrStart    time.Time
	seen         = uint64(0)
	writes       = uint64(0)
	eol          = uint64(0)
	printTxFunc  = printTxTerse
	printRecFunc = printRecTerse
	chanHi       = 0
)

func newIdle(seen uint64, seenLast uint64, t time.Time,
	tLast time.Time) time.Duration {

	if seen-seenLast <= 0 {
		panic("seen - seenLast <= 0")
	}
	newIdle := t.Sub(tLast) / time.Duration(seen-seenLast)
	if newIdle < minIdle {
		newIdle = minIdle
	}
	if newIdle > maxIdle {
		newIdle = maxIdle
	}
	return newIdle
}

func txReader(q *log.Query, txChan chan []log.Tx, statusChan chan log.Status,
	stopChan chan struct{}, wg *sync.WaitGroup) {

	defer wg.Done()
	seenLast := uint64(0)
	idle := maxIdle
	tRdrStart = time.Now()
	tLast := tRdrStart
	for {
		select {
		case <-stopChan:
			close(txChan)
			return
		default:
			break
		}

		txGrp, status := q.NextTxGroup()
		if status == log.EOL {
			eol++
			if (seen & bitmask) > (seenLast & bitmask) {
				t := time.Now()
				idle = newIdle(seen, seenLast, t, tLast)
				seenLast = seen
				tLast = t
			}
			time.Sleep(idle)
			totIdle += idle
			continue
		}
		if status != log.More {
			statusChan <- status
			close(txChan)
			return
		}
		txChan <- txGrp
		chanLen := len(txChan)
		if chanLen > chanHi {
			chanHi = chanLen
		}
		seen++
	}
}

func printTxTerse(tx log.Tx) {
	fmt.Fprintf(out, "<< %-9s>> %-10d\n", tx.Type, tx.VXID)
}

func printTxVerbose(tx log.Tx) {
	fmt.Fprintf(out, "%11s<< %-9s>>   %-10d\n", " ", tx.Type, tx.VXID)
}

func printRecTerse(rec log.Record) {
	fmt.Fprintf(out, "%-14s %s\n", rec.Tag, rec.Payload)
}

func printRecVerbose(rec log.Record) {
	fmt.Fprintf(out, "%10d %-14s %c %s\n", rec.VXID, rec.Tag,
		rune(rec.Type), rec.Payload)
}

func print(txGrp []log.Tx) {
	tx := txGrp[0]
	if len(txGrp) == 1 && txGrp[0].Type == log.TxRaw {
		printRecVerbose(tx.Records[0])
		out.Flush()
		return
	}
	for _, tx := range txGrp {
		if tx.Level > 0 {
			if tx.Level > 3 {
				fmt.Fprintf(out, "*%1.1d*", tx.Level)
			} else {
				fmt.Fprintf(out, "%-3.*s ", tx.Level, "***")
			}
		}
		printTxFunc(tx)
		for _, rec := range tx.Records {
			if tx.Level > 3 {
				fmt.Fprintf(out, "-%1.1d- ", tx.Level)
			} else if tx.Level > 0 {
				fmt.Fprintf(out, "%-3.*s ", tx.Level, "---")
			}
			printRecFunc(rec)
		}
	}
	fmt.Fprintln(out)
	out.Flush()
}

func txWriter(txChan chan []log.Tx, wg *sync.WaitGroup) {
	defer wg.Done()
	for txGrp := range txChan {
		print(txGrp)
		writes++
	}
}

func main() {
	flag.Parse()

	grp, exists := log.Str2Grp[*grpf]
	if !exists {
		fmt.Fprintf(os.Stderr, "Grouping '%s' unknown\n", *grpf)
		os.Exit(-1)
	}

	if *verbose {
		printTxFunc = printTxVerbose
		printRecFunc = printRecVerbose
	}

	l := log.New()
	defer l.Release()
	if err := l.Attach(""); err != nil {
		fmt.Fprintf(os.Stderr, "Attach(default):", err)
		os.Exit(-1)
	}
	c, err := l.NewCursor()
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewCursor():", err)
		os.Exit(-1)
	}
	defer c.Delete()
	q, err := c.NewQuery(grp, *query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewQuery():", err)
		os.Exit(-1)
	}

	txChan := make(chan []log.Tx, 100000)
	statusChan := make(chan log.Status)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	stopChan := make(chan struct{})

	if *cpuprof != "" {
		f, err := os.Create(*cpuprof)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(-1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(-1)
		}
		defer pprof.StopCPUProfile()
	}

	var wg sync.WaitGroup
	wg.Add(2)
	start := time.Now()
	go txReader(q, txChan, statusChan, stopChan, &wg)
	go txWriter(txChan, &wg)

	select {
	case status := <-statusChan:
		fmt.Fprintln(os.Stderr, "status:", status)
		break
	case sig := <-signalChan:
		fmt.Fprintln(os.Stderr, "received signal:", sig)
		stopChan <- struct{}{}
	}
	wg.Wait()

	stop := time.Now()
	out.Flush()

	if *memprof != "" {
		runtime.GC()
		f, err := os.Create(*memprof)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}

	if *stats {
		fmt.Fprintf(os.Stderr, "%v wall clock time\n",
			stop.Sub(start))
		fmt.Fprintf(os.Stderr, "%d tx grps read\n", seen)
		fmt.Fprintf(os.Stderr, "%d tx grps written\n", writes)
		fmt.Fprintf(os.Stderr, "%d r/w channel high watermark\n",
			chanHi)
		fmt.Fprintf(os.Stderr, "%d incomplete tx high watermark\n",
			q.IncompleteTxHigh)
		fmt.Fprintf(os.Stderr, "%d times reader at eol\n", eol)
		fmt.Fprintf(os.Stderr, "%v reader total idle time\n", totIdle)
		fmt.Fprintf(os.Stderr, "%v mean idle time\n",
			totIdle/time.Duration(eol))
		fmt.Fprintf(os.Stderr, "%v reader total non-idle time\n",
			stop.Sub(tRdrStart)-totIdle)
		fmt.Fprintf(os.Stderr, "%v mean non-idle time per tx grp\n",
			(stop.Sub(tRdrStart)-totIdle)/time.Duration(seen))
	}
}
