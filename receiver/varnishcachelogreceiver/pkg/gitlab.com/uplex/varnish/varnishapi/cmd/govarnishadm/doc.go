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

/*
govarnishadm re-implements the varnishadm(1) tool, to demonstrate the
varnishapi admin package.

# SYNOPSIS

govarnishadm [-h] [-n ident] [-t timeout] [-S secretfile] [-T [address]:port] [command [...]]

# DESCRIPTION

The govarnishadm utility establishes a CLI connection to varnishd
either using -n name or using the -T and -S arguments. If -n name is
given the location of the secret file and the address:port is looked
up in shared memory.  If neither is given govarnishadm will look for
an instance without a given name.

If a command is given, the command and arguments are sent over the CLI
connection and the result returned on stdout.

If no command argument is given govarnishadm will pass commands and
replies between the CLI socket and stdin/stdout.

# OPTIONS

These command-line options are available:

	-h|--help

Print program usage and exit.

	-n ident

Connect to the instance of varnishd with this name.

	-S secretfile

Specify the authentication secret file. This should be the same -S
argument as was given to varnishd. Only processes which can read the
contents of this file will be able to authenticate the CLI connection.

	-t timeout

Wait no longer than this many seconds for an operation to finish.

	-T <address:port>

Connect to the management interface at the specified address and port.

The syntax and operation of the actual CLI interface is described in
the varnish-cli(7) manual page. Parameters are described in
varnishd(1) manual page.

Additionally, a summary of commands can be obtained by issuing the
help command, and a summary of parameters can be obtained by issuing
the param.show command.

# EXIT STATUS

If a command is given, the exit status of the govarnishadm utility is
zero if the command succeeded, and non-zero otherwise.

# EXAMPLES

Some ways you can use govarnishadm:

	govarnishadm -T localhost:999 -S /var/db/secret vcl.use foo
	echo vcl.use foo | govarnishadm -T localhost:999 -S /var/db/secret
	echo vcl.use foo | ssh vhost govarnishadm -T localhost:999 -S /var/db/secret

# DIFFERENCES FROM VARNISHADM

govarnishadm does not have the command-line completion that may be
possible for varnishadm (if an appropriate library is linked).

govarnishadm cannot detect if it is connected to a tty. So if commands
are piped into stdin, the output in stdout will have the banner and
prompts that appear in interactive mode.

# SEE ALSO

* varnishd(1)

* varnish-cli(7)

# AUTHORS

The varnishadm utility and its manual page, from which much of the
present documentation was adapted, were written by Cecilie Fritzvold.
The man page was later modified by Per Buer, Federico G.  Schwindt and
Lasse Karstensen.

govarnishadm was written by Geoff Simmons.
*/
package main
