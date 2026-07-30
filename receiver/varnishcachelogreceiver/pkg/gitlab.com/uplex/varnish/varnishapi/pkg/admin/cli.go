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

package admin

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// The same "nonsense" string used by varnishtest (as of 6.1)
	// as the delimiter for vcl.inline commands used to run tests.
	nonsense = "%XJEIFLH|)Xspa8P"
)

// Pong represents the response to the "ping" command.
type Pong struct {
	Time       time.Time
	CLIVersion string
}

// Ping encapsulates the "ping" command. error is non-nil when the
// response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
func (adm *Admin) Ping() (Pong, error) {
	pong := Pong{}
	resp, err := adm.Command("ping")
	if err != nil {
		return pong, err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return pong, badResp
	}
	answer := strings.Split(resp.Msg, " ")
	epoch, err := strconv.Atoi(answer[1])
	if err != nil {
		return pong, err
	}
	pong.Time = time.Unix(int64(epoch), 0)
	pong.CLIVersion = answer[2]
	return pong, err
}

// A State represents the status of the Varnish child process.
type State uint8

const (
	// Stopped indicates that the child was stopped regularly.
	Stopped State = iota

	// Starting indicates that the management process has begun
	// but not completed starting the child process.
	Starting

	// Running indicates that the child process is running
	// regularly.
	Running

	// Stopping indicates that the management process is shutting
	// down the child process regularly.
	Stopping

	// Died indicates that the child process process stopped
	// unexpectedly, and will be restarted by the management
	// process.
	Died
)

func (state State) String() string {
	switch state {
	case Stopped:
		return "stopped"
	case Starting:
		return "starting"
	case Running:
		return "running"
	case Stopping:
		return "stopping"
	case Died:
		return "died unexpectedly (being restarted)"
	default:
		panic("Invalid state")
	}
}

// Status encapsulates the "status" command. error is non-nil when the
// response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
func (adm *Admin) Status() (State, error) {
	resp, err := adm.Command("status")
	if err != nil {
		return Died, err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return Died, badResp
	}
	if strings.Contains(resp.Msg, "running") {
		return Running, nil
	}
	if strings.Contains(resp.Msg, "stopped") {
		return Stopped, nil
	}
	if strings.Contains(resp.Msg, "starting") {
		return Starting, nil
	}
	if strings.Contains(resp.Msg, "stopping") {
		return Stopping, nil
	}
	if strings.Contains(resp.Msg, "died") {
		return Died, nil
	}
	return Died, errors.New("unknown status")
}

// Start encapsulates the "start" command. error is non-nil when the
// response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
func (adm *Admin) Start() error {
	resp, err := adm.Command("start")
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// Stop encapsulates the "stop" command. error is non-nil when the
// response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
func (adm *Admin) Stop() error {
	resp, err := adm.Command("stop")
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLLoad encapsulates the "vcl.load" command. error is non-nil when
// the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// Varnish usually returns status 106 (Param) when the load fails due
// to a compile failure, and the error message is in the wrapped
// Response.Msg.
//
// config is the configuration name under which the VCL instance can
// be referenced afterward. The name may not be already in use. path
// is file path at which the VCL source is located.
func (adm *Admin) VCLLoad(config string, path string) error {
	resp, err := adm.Command("vcl.load", config, path)
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLInline encapsulates the "vcl.inline" command. error is non-nil
// when the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// As with VCLLoad, Varnish will typically return status 106 (Param)
// when the VCL source cannot be compiled, and the error message is in
// the wrapped Response.Msg.
//
// config is the configuration name under which the VCL instance can
// be referenced afterward. The name may not be already in use. src is
// a string containing a literal VCL source.
func (adm *Admin) VCLInline(config string, src string) error {
	hereSrc := "<< " + nonsense + "\n" + src + "\n" + nonsense
	resp, err := adm.Command("vcl.inline", config, hereSrc)
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLUse encapsulates the "vcl.use" command. error is non-nil when
// the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// config is the configuration name previousy set with VCLLoad(), or a
// previous invocation of "vcl.load". On success, that config becomes
// the active VCL instance.
func (adm *Admin) VCLUse(config string) error {
	resp, err := adm.Command("vcl.use", config)
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLLabel encapsulates the "vcl.label" command. error is non-nil
// when the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// label is the VCL label name, which may have previously defined by
// VCLLabel() or an invocation of "vcl.label", or which becomes
// defined through the use of this function. config is the
// configuration name previousy set with VCLLoad(), or a previous
// invocation of "vcl.load". On success, the label is used to refer to
// the config.
func (adm *Admin) VCLLabel(label string, config string) error {
	resp, err := adm.Command("vcl.label", label, config)
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLDiscard encapsulates the "vcl.discard" command. error is non-nil
// when the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// config is the configuration name previousy set with VCLLoad(), or a
// previous invocation of "vcl.load". The config may not be the
// current active config. On success, that config is no longer
// available, and cannot become active via VCLUse() or "vcl.use".
func (adm *Admin) VCLDiscard(config string) error {
	resp, err := adm.Command("vcl.discard", config)
	if err != nil {
		return err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return badResp
	}
	return nil
}

// VCLStatus encodes whether a VCL configuration is the one in active
// use by a Varnish instance, having been specified by VCLUse() or the
// vcl.use command. It corresponds to the first column of vcl.list
// output.
type VCLStatus uint8

const (
	// Active indicates that a VCL has been specified by vcl.use.
	Active VCLStatus = iota

	// Available indicates that a VCL has been loaded, but is not
	// active.
	Available

	// Discarded indicates that a VCL has been marked for removal,
	// having been specified by VCLDiscard() or the vcl.discard
	// command.
	Discarded
)

// String returns the string for a VCLStatus that appears in the first
// column of vcl.list output.
func (status VCLStatus) String() string {
	switch status {
	case Active:
		return "active"
	case Available:
		return "available"
	case Discarded:
		return "discarded"
	default:
		panic("Invalid status")
	}
}

// VCLState encodes the state of a VCL configuration or label. It
// corresponds the string before the slash in the second column of
// vcl.list output.
//
// If a VCL's state is Label, then it is a label that has been applied
// to a configuration with VCLLabel() or the vcl.label
// command. Otherwise it is a configuration that has been loaded with
// VCLLoad() or VCLInline(), or one of the vcl.load or vcl.inline
// commands.
type VCLState uint8

const (
	// Auto indicates a VCL configuration that undergoes
	// temperature transitions automatically -- it remains warm
	// when not in use for the duration of the cooldown period
	// (set by the varnishd parameter vcl_cooldown).
	Auto VCLState = iota

	// ColdState indicates a VCL configuration in the cold state
	// -- it has released resources, including removal of backend
	// definitions and running VMOD finalizers.
	ColdState

	// WarmState indicates a VCL configuration in the warm state
	// -- even while inactive, resources are not yet released (so
	// so that it can be quickly made active again).
	WarmState

	// Label indicates a VCL label, applied to a configuration
	// with VCLLabel() or the vcl.label command.
	Label
)

// String returns the string for a VCLState that appears in the second
// column of vcl.list output.
func (state VCLState) String() string {
	switch state {
	case Auto:
		return "auto"
	case ColdState:
		return "cold"
	case WarmState:
		return "warm"
	case Label:
		return "label"
	default:
		panic("Invalid state")
	}
}

// VCLTemperature encodes information about the initialized or
// released state of a VCL configuration. It corresponds the string
// after the slash in the second column of vcl.list output.
type VCLTemperature uint8

const (
	// Init indicates that a VCL instance is initializing (for
	// example, it may be running VMOD object constructors).
	Init VCLTemperature = iota

	// ColdTemp indicates that the VCL instance is in the cold
	// state -- it has released resources, including removal of
	// backend definitions and running VMOD finalizers.
	ColdTemp

	// WarmTemp indicates a VCL configuration in the warm state --
	// even while inactive, resources are not yet released (so so
	// that it can be quickly made active again).
	WarmTemp

	// Busy indicates that the VCL instance is transitioning to
	// the cold state, but there are still active threads using
	// the configuration.
	Busy

	// Cooling indicates a VCL instance in the transition to the
	// cold state that is not in use by any thread, but has not
	// yet completed releasing resources.
	Cooling
)

// String returns the string for a VCLTemperature that appears in the
// second column of vcl.list output.
func (temp VCLTemperature) String() string {
	switch temp {
	case Init:
		return "init"
	case ColdTemp:
		return "cold"
	case WarmTemp:
		return "warm"
	case Busy:
		return "busy"
	case Cooling:
		return "cooling"
	default:
		panic("Invalid temperature")
	}
}

// VCLData represents information about loaded VCL configurations and
// labels that is returned by the vcl.list command.
//
// If the value of the State field is Label, then this object
// represents a VCL label, applied to a configuration with VCLLabel()
// or the vcl.label command. In that case, the Labels field does not
// apply, but the LabelVCL and LabelReturns fields are valid.
//
// With any other value for the State field, the object represents a
// VCL configuration, loaded with VCLLoad() or VCLInline(), or one of
// the vcl.load or vcl.inline commands. In that case the Labels field
// is valid, but the LabelVCL and LabelReturns fields are not.
type VCLData struct {
	// Name is the name of the VCL configuration or label, given
	// when the object was created. It corresponds to the fourth
	// column of vcl.list output.
	Name string

	// Status indicates whether a VCL configuration is in active
	// use, having been specified by VCLUse() or vcl.use. It
	// corresponds to the first column of vcl.list output.
	Status VCLStatus

	// State indicates the state of the configuration or label.
	// It is a label if and only if State==Label. The field
	// corresponds to the string before the slash in the second
	// column of vcl.list output.
	State VCLState

	// Temperature indicates the initialized or released state of
	// a VCL configuration. It corresponds to the string after the
	// slash in the second column of vcl.list output.
	Temperature VCLTemperature

	// Busy indicates the number of resources using the VCL
	// configuration, such as client sessions or backend
	// fetches. A VCL instance cannot transition to the cold state
	// or be discarded until the busy count reaches 0. The field
	// corresponds to the number in the third column of vcl.list
	// output
	Busy uint

	// LabelVCL is only valid for a label (State==Label). It is
	// the name of the VCL configuration to which the label is
	// applied.
	LabelVCL string

	// LabelReturns is only valid for a label (State==Label),
	// indicating the number of return(vcl) statements in VCL
	// configurations that branch to the label.
	LabelReturns uint

	// Labels is only valid for a VCL configuration
	// (State!=Label), indicating the number of labels that have
	// been applied to it.
	Labels uint
}

var (
	str2status = map[string]VCLStatus{
		"active":    Active,
		"available": Available,
		"discarded": Discarded,
	}
	str2state = map[string]VCLState{
		"auto":  Auto,
		"cold":  ColdState,
		"warm":  WarmState,
		"label": Label,
	}
	str2temp = map[string]VCLTemperature{
		"init":    Init,
		"cold":    ColdTemp,
		"warm":    WarmTemp,
		"busy":    Busy,
		"cooling": Cooling,
	}
	vclMain = regexp.MustCompile(
		`^(\w+)\s+(\w+)(?:/|\s+)(\w+)\s+(\d+|-)\s+(\S+)`)
	vclLbls    = regexp.MustCompile(`->\s+(\S+)\s+\((\d+)`)
	vclLblRefs = regexp.MustCompile(`(\d+)\s+labels?\)\s*$`)
)

// VCLList encapsulates the "vcl.list" command. error is non-nil when
// the response was not OK. In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// When error is nil, VCLList() returns a slice of VCLData objects --
// structured data encoding the information in the output of vcl.list.
func (adm *Admin) VCLList() ([]VCLData, error) {
	data := []VCLData{}
	resp, err := adm.Command("vcl.list")
	if err != nil {
		return data, err
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return data, badResp
	}

	vcls := strings.Split(resp.Msg, "\n")
	for _, vcl := range vcls {
		datum := VCLData{}
		if main := vclMain.FindStringSubmatch(vcl); main != nil {
			if status, ok := str2status[main[1]]; ok {
				datum.Status = status
			} else {
				return data,
					fmt.Errorf("Unknown status %s: %s",
						main[1], vcl)
			}
			if state, ok := str2state[main[2]]; ok {
				datum.State = state
			} else {
				return data,
					fmt.Errorf("Unknown state %s: %s",
						main[2], vcl)
			}
			if temp, ok := str2temp[main[3]]; ok {
				datum.Temperature = temp
			} else {
				return data,
					fmt.Errorf("Unknown temperature %s: %s",
						main[3], vcl)
			}
			if main[4] == "-" {
				datum.Busy = 0
			} else if b, e := strconv.ParseUint(main[4], 0, 0); e == nil {
				datum.Busy = uint(b)
			} else {
				return data,
					fmt.Errorf("Cannot parse busy field %s"+
						" in '%s': %v", main[4], vcl, e)
			}
			datum.Name = main[5]
		} else {
			return data,
				fmt.Errorf("Cannot parse vcl.list output: %s",
					vcl)
		}
		if datum.State != Label {
			if r := vclLblRefs.FindStringSubmatch(vcl); r != nil {
				if n, err := strconv.ParseUint(r[1], 0, 0); err == nil {
					datum.Labels = uint(n)
				} else {
					return data,
						fmt.Errorf("Cannot parse "+
							"labels field %s in "+
							"'%s': %v", r[1], vcl,
							err)
				}
			}
		} else {
			if l := vclLbls.FindStringSubmatch(vcl); l != nil {
				datum.LabelVCL = l[1]
				if n, err := strconv.ParseUint(l[2], 0, 0); err == nil {
					datum.LabelReturns = uint(n)
				} else {
					return data,
						fmt.Errorf("Cannot parse "+
							"returns field %s in "+
							"'%s': %v", l[2], vcl,
							err)
				}
			} else {
				return data,
					fmt.Errorf("Cannot parse vcl.list "+
						"output: %s", vcl)
			}
		}
		data = append(data, datum)
	}
	return data, nil
}

// ActiveVCL returns data about the active VCL configuration, as
// indicated by the output of the vcl.list command.  error is non-nil
// if the response to vcl.list was not OK. In that case, the error is
// an UnexpectedResponse, which wraps the response from Varnish.
//
// When error is nil, ActiveVCL() returns a VCLData object --
// structured data encoding the information in the output of vcl.list.
// If no configuration is currently active, all fields of the VCLData
// object are set to their zero values; for example, the Name field is
// empty (which is otherwise never the case).
func (adm *Admin) ActiveVCL() (VCLData, error) {
	vcl := VCLData{}
	data, err := adm.VCLList()
	if err != nil {
		return vcl, err
	}
	for _, datum := range data {
		if datum.Status == Active {
			return datum, nil
		}
	}
	return vcl, nil
}

// GetPanic encapsulates the "panic.show" command. error is non-nil
// when the response was neither of OK or Cant (Cant is the status
// when there was no panic to show). In that case, the error is an
// UnexpectedResponse, which wraps the response from Varnish.
//
// When error is nil, the most recent panic string is returned, or the
// empty string if there has been no panic since Varnish started or
// since the panic was cleared.
func (adm *Admin) GetPanic() (string, error) {
	resp, err := adm.Command("panic.show")
	if err != nil {
		return "", err
	}
	if resp.Code == Cant {
		// Don't panic!
		return "", nil
	}
	if resp.Code != OK {
		badResp := UnexpectedResponse{Response: resp}
		return "", badResp
	}
	return resp.Msg, nil
}
