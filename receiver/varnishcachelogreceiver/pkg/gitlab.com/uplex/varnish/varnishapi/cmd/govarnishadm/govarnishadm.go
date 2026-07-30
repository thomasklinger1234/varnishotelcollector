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
	"gitlab.com/uplex/varnish/varnishapi/pkg/admin"
	"gitlab.com/uplex/varnish/varnishapi/pkg/vsm"

	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

var (
	instance = flag.String("n", "", "varnish instance name")
	timeout  = flag.Uint("t", 0, "timeout")
	tflag    = flag.String("T", "", "[address]:port")
	sflag    = flag.String("S", "", "secretfile")
)

func errExit(err error) {
	fmt.Println(err)
	os.Exit(-1)
}

func main() {
	var adm *admin.Admin

	flag.Parse()

	tmo := time.Duration(*timeout * uint(time.Second))

	if *tflag == "" || *sflag == "" {
		vsm := vsm.New()
		if vsm == nil {
			fmt.Println("Cannot initiate attachment to Varnish " +
				"shared memory")
			os.Exit(-1)
		}
		defer vsm.Destroy()
		if err := vsm.Attach(*instance); err != nil {
			errExit(err)
		}
		if *tflag == "" {
			addr, err := vsm.GetMgmtAddr()
			if err != nil {
				errExit(err)
			}
			*tflag = addr
		}
		if *sflag == "" {
			sarg, err := vsm.GetSecretPath()
			if err != nil {
				errExit(err)
			}
			*sflag = sarg
		}
	}

	sfile, err := os.Open(*sflag)
	if err != nil {
		errExit(err)
	}
	secret, err := ioutil.ReadAll(sfile)
	if err != nil {
		errExit(err)
	}
	adm, err = admin.Dial(*tflag, secret, tmo)
	if err != nil {
		errExit(err)
	}

	if len(flag.Args()) > 0 {
		resp, err := adm.Command(flag.Args()...)
		if err != nil {
			errExit(err)
		}
		fmt.Println(resp.Msg)
		if resp.Code != admin.OK {
			fmt.Printf("Command failed with error code %d\n",
				resp.Code)
			os.Exit(-1)
		}
		os.Exit(0)
	}

	fmt.Println(adm.Banner)
	fmt.Println()
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("varnish> ")
		input, err := reader.ReadString('\n')

		if err != nil {
			if err == io.EOF {
				fmt.Println()
				os.Exit(0)
			}
			errExit(err)
		}
		input = strings.TrimSpace(input)
		cmds := strings.Split(input, " ")

		if tmo > 0 {
			err := adm.SetReadDeadline(time.Now().Add(tmo))
			if err != nil {
				fmt.Println("Error setting read timeout:", err)
			}
		}
		resp, err := adm.Command(cmds...)
		if err != nil {
			errExit(err)
		}

		fmt.Println(int(resp.Code))
		fmt.Println(resp.Msg)
		if resp.Code == admin.Close {
			os.Exit(0)
		}
		fmt.Println()
	}
}
