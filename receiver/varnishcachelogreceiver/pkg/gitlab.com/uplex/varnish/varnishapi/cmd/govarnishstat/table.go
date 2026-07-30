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

const table = `<!DOCTYPE html>
<html>
<head>
<title>govarnishstat</title>

<style>
table {
    border-collapse: collapse;
    font-family: monospace,monospace;
}

table, td, th {
    border: 2px solid black;
}

td.name, th.name {
    width: 24em;
    text-align: left;
}

td, th {
    width: 14em;
    text-align: right;
    padding: 0.25rem;
}

th {
    background-color: black;
    color: white;
}

tr:hover {background-color: WhiteSmoke;}

.hidden { display: none; }

table.uptime {
    float: left;
    text-align: left;
    width: 18em;
}

table.hitrate {
    float: right;
    width: 23em;
}

.top {
    border: none;
}

#stats {
    clear: both;
}

#wrapper {
    display: inline-block;
}

.controls {
    font-family: monospace,monospace;
    margin: 0 10px;
    float: right;
}

#tooltip {
    position: absolute;
    border-style: dotted;
    font-family: monospace,monospace;
}

#tipName {
    font-weight: bold;
}

.tiptext {
    margin-bottom: 0.5rem;
}

#longDesc {
    display:block;
    max-width: 24em;
}
</style>

<script type="text/javascript">
var addr = "ws://" + location.hostname + ":" + location.port
var metadata = {}
var tbody
var rows
var colClassList = []
var show0 = false
var show0_checkbox
var formatting = true
var scaling_checkbox
var verbosity = "INFO"
var verbose_select
var dot = ".&nbsp;&nbsp;&nbsp;"
var uptime_mgt
var uptime_child
var suffix = ["&nbsp;", "K", "M", "G", "T", "P", "E", "Z", "Y"]
var tbl_built = false
var tooltip
var tipName
var shortDesc

function Metadata(short, long, level, format, semantics) {
	this.short = short
	this.long = long
	this.level = level
	this.format = format
	this.semantics = semantics
}

function format_num(cell, classList) {
	var val = cell.getAttribute("data-raw")
	if (classList.contains("avg")) {
		cell.innerHTML = parseFloat(val).toFixed(2) + "&nbsp;"
		return
	}
	cell.innerHTML = val + "&nbsp;"
}

function format_duration(cell) {
	var val = cell.getAttribute("data-raw")
	var days = Math.floor(val/86400).toFixed(0)
	var hours = (Math.floor((val % 86400)/3600)).toFixed(0)
	if (hours < 10) {
		hours = "0" + hours
	}
	var mins = (Math.floor((val % 3600)/60)).toFixed(0)
	if (mins < 10) {
		mins = "0" + mins
	}
	secs = (val % 60).toFixed(0)
	if (secs < 10) {
		secs = "0" + secs
	}
	cell.innerHTML = days + "+" + hours + ":" + mins + ":" + secs + "&nbsp;"
}

function format_bytes(cell, classList) {
	var val = parseFloat(cell.getAttribute("data-raw"))
	var i
	if (classList.contains("int") && val < 1024) {
		cell.innerHTML = val.toFixed(0) + "&nbsp;"
		return
	}
	for (i = 0; val >= 1024; i++)
		val /= 1024
	cell.innerHTML = val.toFixed(2) + suffix[i]
}

function format(cell, format, semantics, cur0, classList) {
	if (classList.contains("avg")) {
		switch (format) {
		case "bitmap":
		case "duration":
			return
		}
		if ((cur0 && !classList.contains("moving")) ||
		    (semantics == "gauge" && classList.contains("uptime"))) {
			cell.innerHTML = dot
			return
		}
	}
	if (format == "bitmap") {
		cell.innerHTML = cell.getAttribute("data-raw") + "&nbsp;"
		return
	}
	if (formatting)
		switch (format) {
		case "duration":
			format_duration(cell)
			return
		case "bytes":
			format_bytes(cell, classList)
			return
		}
	format_num(cell, classList)
}

function format_row(row) {
	var meta = metadata[row.id]
	var cur0 = row.cells[1].getAttribute("data-raw") == "0"
	for (i = 1; i <= 6; i++)
		format(row.cells[i], meta.format, meta.semantics, cur0,
		       colClassList[i])
}

function getD9ns() {
	var ws = new WebSocket(addr + "/d9ns")
	ws.onmessage = function (evt) {
		var d9ns = JSON.parse(evt.data)
		for (i = 0; i < d9ns.length; i++) {
			var row = tbody.insertRow(i)
			var nameCell = row.insertCell(0)
			var curCell = row.insertCell(1)
			var changeCell = row.insertCell(2)
			var avgCell = row.insertCell(3)
			var avg10Cell = row.insertCell(4)
			var avg100Cell = row.insertCell(5)
			var avg1000Cell = row.insertCell(6)

			var d9n = d9ns[i]
			metadata[d9n.name] = new Metadata(d9n.short, d9n.long,
			                                  d9n.level, d9n.format,
			                                  d9n.semantics)
			if (d9n.semantics == "gauge") {
				avgCell.innerHTML = ".&nbsp;&nbsp;&nbsp;"
			}
			row.id = d9n.name
			nameCell.innerHTML = d9n.name
			nameCell.className = "name"

			row.classList.add(d9n.level)
			if (d9n.level != "INFO") {
				row.classList.add("hidden")
			}
			row.addEventListener("mouseover", showDesc)
			row.addEventListener("mouseout", hideDesc)
		}
		tbl_built = true
	};
}

function getStats() {
	var ws = new WebSocket(addr + "/stats")
	ws.onmessage = function (evt) {
		if (!tbl_built)
			return
		var stats = JSON.parse(evt.data)
		var row = document.getElementById(stats.name)
		var cells = row.cells
		if (metadata[row.id].semantics != "bitmap") {
			cells[1].setAttribute("data-raw", stats.value)
		}
		else {
			cells[1].setAttribute("data-raw", stats.bitmap)
		}
		cells[2].setAttribute("data-raw", stats.change)
		cells[3].setAttribute("data-raw", stats.avg)
		cells[4].setAttribute("data-raw", stats.avg10)
		cells[5].setAttribute("data-raw", stats.avg100)
		cells[6].setAttribute("data-raw", stats.avg1000)
		if (!show0 && stats.value == 0) {
			row.classList.add("hidden")
		}
		format_row(row)

		if (stats.name == "MGT.uptime") {
			uptime_mgt.setAttribute("data-raw", stats.value)
			format_duration(uptime_mgt)
		}
		if (stats.name == "MAIN.uptime") {
			uptime_child.setAttribute("data-raw", stats.value)
			format_duration(uptime_child)
		}
		ws.send("ACK");
	};
}

function format_hitrate(id, val) {
	var cell = document.getElementById(id)
	cell.innerHTML = val.toFixed(4)
}

function getHitrate() {
	var ws = new WebSocket(addr + "/hitrate")
	ws.onmessage = function (evt) {
		var stats = JSON.parse(evt.data)
		format_hitrate("hitrate10", stats.avg10)
		format_hitrate("hitrate100", stats.avg100)
		format_hitrate("hitrate1000", stats.avg1000)
		ws.send("ACK");
	};
}

function resetVisibility() {
	for (i = 0; i < rows.length; i++) {
		var raw = rows[i].getAttribute("data-raw")
		if (raw == "0" && !show0) {
			rows[i].classList.add("hidden")
			continue
		}
		if (rows[i].classList.contains("INFO")) {
			rows[i].classList.remove("hidden")
			continue
		}
		switch(verbosity) {
		case "INFO":
			if (rows[i].classList.contains("DIAG")) {
				rows[i].classList.add("hidden")
				continue
			}
			if (rows[i].classList.contains("DEBUG")) {
				rows[i].classList.add("hidden")
				continue
			}
		case "DIAG":
			if (rows[i].classList.contains("DIAG")) {
				rows[i].classList.remove("hidden")
				continue
			}
			if (rows[i].classList.contains("DEBUG")) {
				rows[i].classList.add("hidden")
				continue
			}
		case "DEBUG":
			if (rows[i].classList.contains("DIAG")) {
				rows[i].classList.remove("hidden")
				continue
			}
			if (rows[i].classList.contains("DEBUG")) {
				rows[i].classList.remove("hidden")
				continue
			}
		}
	}
}

function setVerbosity() {
	verbosity = verbose_select.value
	resetVisibility()
}

function toggleShow0() {
	show0 = !show0
	resetVisibility()
}

function toggleFormatting() {
	formatting = !formatting
}

function keyUp(evt) {
	var k = evt.which || evt.keyCode;
	switch(k) {
	case 68:	/* d */
		if (!evt.shiftKey) {
			show0_checkbox.checked = !show0_checkbox.checked
			toggleShow0()
		}
		return
	case 69:	/* e */
		if (!evt.shiftKey) {
			scaling_checkbox.checked = !scaling_checkbox.checked
			toggleFormatting()
		}
		return
	case 86:	/* v */
		if (!evt.shiftKey)
			switch (verbosity) {
			case "INFO":
				verbose_select.selectedIndex = 1
				break
			case "DIAG":
				verbose_select.selectedIndex = 2
				break
			}
		else
			switch (verbosity) {
			case "DIAG":
				verbose_select.selectedIndex = 0
				break
			case "DEBUG":
				verbose_select.selectedIndex = 1
				break
			}
		setVerbosity()
		return
	}
}

function showDesc(evt) {
	var row = evt.currentTarget
	var name = row.id
	var meta = metadata[name]
	console.log(name + ": " + meta.long)
	tipName.innerHTML = name + " [" + meta.level + "]"
	shortDesc.innerHTML = meta.short
	longDesc.innerHTML = meta.long
	tooltip.style.top = tbody.offsetTop + row.offsetTop + "px"
	tooltip.style.left = (row.offsetWidth * 1.01 + row.offsetLeft) + "px"
	tooltip.classList.remove("hidden")
}

function hideDesc(evt) {
	tooltip.classList.add("hidden")
}

window.onload = function() {
	var tbl = document.getElementById("statsTbl")
	tbody = tbl.getElementsByTagName('tbody')[0];
	rows = tbody.rows
	var headrow = document.getElementById("headrow")
	for (i = 0; i < headrow.cells.length; i++)
		colClassList[i] = headrow.cells[i].classList
	getD9ns()
	getStats()
	getHitrate()
	uptime_mgt = document.getElementById("uptime_mgt")
	uptime_child = document.getElementById("uptime_child")
	verbose_select = document.getElementById("verbosity")
	verbose_select.addEventListener("change", setVerbosity);
	show0_checkbox = document.getElementById("show0")
	show0_checkbox.onclick = toggleShow0
	document.addEventListener("keyup", keyUp);
	scaling_checkbox = document.getElementById("scaling")
	scaling_checkbox.onclick = toggleFormatting
	tooltip = document.getElementById("tooltip")
	tipName = document.getElementById("tipName")
	shortDesc = document.getElementById("shortDesc")
}
</script>

</head>

<body>
<div id="wrapper">
	<div id="header">
		<table class="uptime top">
		<tr class="top">
			<td class="top">Uptime mgt:</td>
			<td class="top" id="uptime_mgt"></td>
		</tr>
		<tr class="top">
			<td class="top">Uptime child:</td>
			<td class="top" id="uptime_child"></td>
		</tr>
		</table>

		<table class="hitrate top">
		<tr>
			<td class="top">Hitrate n:</td>
			<td class="top">10</td>
			<td class="top">100</td>
			<td class="top">1000</td>
		</tr>
		<tr class="top">
			<td class="top">avg(n):</td>
			<td class="top" id="hitrate10"></td>
			<td class="top" id="hitrate100"></td>
			<td class="top" id="hitrate1000"></td>
		</tr>
		</table>
	</div>
	<div id="stats">
	<br />
	<table id="statsTbl">
		<thead>
			<tr id="headrow">
				<th class="name">NAME</th>
				<th class="int">CURRENT&nbsp;</th>
				<th class="avg current">CHANGE&nbsp;</th>
				<th class="avg uptime">AVERAGE&nbsp;</th>
				<th class="avg moving">AVG_10&nbsp;</th>
				<th class="avg moving">AVG_100&nbsp;</th>
				<th class="avg moving">AVG_1000&nbsp;</th>
			</tr>
		</thead>
		<tbody>
		<div id="tooltip" class="hidden">
			<div class="tiptext">
				<span id="tipName"></span>
			</div>
			<div class="tiptext">
				<span id="shortDesc"></span>
			</div>
			<div class="tiptext">
				<span id="longDesc"></span>
			</div>
		</div>
		</tbody>
	</table>
	</div>
	<div id="controls">
		<div class="controls">
		<p>Verbosity</p>
		<select id="verbosity">
			<option value="INFO">INFO</option>
			<option value="DIAG">DIAG</option>
			<option value="DEBUG">DEBUG</option>
		</select>
		</div>
		<div class="controls">
		<p>Current=0</p>
		<input type="checkbox" id="show0">Show
		</div>
		<div class="controls">
		<p>&nbsp;</p>
		<input type="checkbox" id="scaling" checked>Scaling
		</div>
	</div>
</div>


</body>
</html>`
