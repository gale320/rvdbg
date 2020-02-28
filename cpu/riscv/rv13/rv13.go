//-----------------------------------------------------------------------------
/*

RISC-V Debugger 0.13 Functions

*/
//-----------------------------------------------------------------------------

package rv13

import (
	"errors"
	"fmt"
	"strings"

	"github.com/deadsy/rvdbg/bitstr"
	"github.com/deadsy/rvdbg/cpu/riscv/rv"
	"github.com/deadsy/rvdbg/jtag"
	"github.com/deadsy/rvdbg/util"
)

//-----------------------------------------------------------------------------

const irDtmcs = 0x10 // debug transport module control and status
const irDmi = 0x11   // debug module interface access

const drDtmcsLength = 32

//-----------------------------------------------------------------------------

// Debug is a RISC-V 0.13 debugger.
// It implements the rv.Debug interface.
type Debug struct {
	dev             *jtag.Device
	hart            []*hartInfo // implemented harts
	hartid          int         // currently selected hart
	ir              uint        // cache of ir value
	irlen           int         // IR length
	drDmiLength     int         // DR length for dmi
	abits           uint        // address bits in dtmcs
	idle            uint        // idle value in dtmcs
	progbufsize     uint        // number of progbuf words implemented
	datacount       uint        // number of data words implemented
	autoexecprogbuf bool        // can we autoexec on progbufX access?
	autoexecdata    bool        // can we autoexec on dataX access?
	sbasize         uint        // width of system bus address (0 = no access)
	hartsellen      uint        // hart select length 0..20
	impebreak       uint        // implicit ebreak in progbuf
}

func (dbg *Debug) String() string {
	s := []string{}
	s = append(s, fmt.Sprintf("version 0.13"))
	s = append(s, fmt.Sprintf("idle cycles %d", dbg.idle))
	s = append(s, fmt.Sprintf("sbasize %d bits", dbg.sbasize))
	s = append(s, fmt.Sprintf("progbufsize %d words", dbg.progbufsize))
	s = append(s, fmt.Sprintf("datacount %d words", dbg.datacount))
	s = append(s, fmt.Sprintf("autoexecprogbuf %t", dbg.autoexecprogbuf))
	s = append(s, fmt.Sprintf("autoexecdata %t", dbg.autoexecdata))
	s = append(s, fmt.Sprintf("hartsellen %d bits", dbg.hartsellen))
	s = append(s, fmt.Sprintf("impebreak %d", dbg.impebreak))
	for i := range dbg.hart {
		s = append(s, fmt.Sprintf("\n%s", dbg.hart[i]))
	}
	return strings.Join(s, "\n")
}

// New returns a RISC-V 0.13 debugger.
func New(dev *jtag.Device) (rv.Debug, error) {

	dbg := &Debug{
		dev:   dev,
		irlen: dev.GetIRLength(),
	}

	// get dtmcs
	dtmcs, err := dbg.rdDtmcs()
	if err != nil {
		return nil, err
	}
	// check the version
	if util.Bits(dtmcs, 3, 0) != 1 {
		return nil, errors.New("unknown dtmcs version")
	}
	// get the dmi address bits
	dbg.abits = util.Bits(dtmcs, 9, 4)
	// get the cycles to wait in run-time/idle.
	dbg.idle = util.Bits(dtmcs, 14, 12)

	// check dmi for the correct length
	dbg.drDmiLength = 33 + int(dbg.abits) + 1
	_, err = dev.CheckDR(irDmi, dbg.drDmiLength)
	if err != nil {
		return nil, err
	}

	// reset the debug module
	err = dbg.wrDtmcs(dmihardreset | dmireset)
	if err != nil {
		return nil, err
	}

	// make the dmi active
	err = dbg.wrDmi(dmcontrol, 0)
	if err != nil {
		return nil, err
	}
	err = dbg.wrDmi(dmcontrol, dmactive)
	if err != nil {
		return nil, err
	}

	// write all-ones to hartsel
	err = dbg.selectHart((1 << 20) - 1)
	if err != nil {
		return nil, err
	}

	// read back dmcontrol
	x, err := dbg.rdDmi(dmcontrol)
	if err != nil {
		return nil, err
	}
	// check dmi is active
	if x&dmactive == 0 {
		return nil, errors.New("dmi is not active")
	}
	// work out hartsellen
	hartsel := getHartSelect(x)
	for hartsel&1 != 0 {
		dbg.hartsellen++
		hartsel >>= 1
	}

	// check dmstatus fields
	x, err = dbg.rdDmi(dmstatus)
	if err != nil {
		return nil, err
	}
	// check version
	if util.Bits(uint(x), 3, 0) != 2 {
		return nil, errors.New("unknown dmstatus version")
	}
	// check authentication
	if util.Bit(uint(x), 7) != 1 {
		return nil, errors.New("debugger is not authenticated")
	}
	// implicit ebreak after progbuf
	dbg.impebreak = util.Bit(uint(x), 22)

	// work out the system bus address size
	x, err = dbg.rdDmi(sbcs)
	if err != nil {
		return nil, err
	}
	dbg.sbasize = util.Bits(uint(x), 11, 5)

	// work out how many program and data words we have
	x, err = dbg.rdDmi(abstractcs)
	if err != nil {
		return nil, err
	}
	dbg.progbufsize = util.Bits(uint(x), 28, 24)
	dbg.datacount = util.Bits(uint(x), 3, 0)

	// check progbuf/impebreak consistency
	if dbg.progbufsize == 1 && dbg.impebreak != 1 {
		return nil, fmt.Errorf("progbufsize == 1 and impebreak != 1")
	}

	// work out if we can autoexec on progbuf/data access
	err = dbg.wrDmi(abstractauto, 0xffffffff)
	if err != nil {
		return nil, err
	}
	x, err = dbg.rdDmi(abstractauto)
	if err != nil {
		return nil, err
	}
	if util.Bits(uint(x), 31, 16) == ((1 << dbg.progbufsize) - 1) {
		dbg.autoexecprogbuf = true
	}
	if util.Bits(uint(x), 11, 0) == ((1 << dbg.datacount) - 1) {
		dbg.autoexecdata = true
	}

	// enumerate the harts
	maxHarts := 1 << dbg.hartsellen
	for id := 0; id < maxHarts; id++ {
		// select the hart
		err := dbg.selectHart(id)
		if err != nil {
			return nil, err
		}
		// get the hart status
		x, err = dbg.rdDmi(dmstatus)
		if err != nil {
			return nil, err
		}
		if x&anynonexistent != 0 {
			// this hart does not exist - we're done
			break
		}
		// add a hart to the list
		dbg.hart = append(dbg.hart, dbg.newHart(id))
	}

	if len(dbg.hart) == 0 {
		return nil, errors.New("no harts found")
	}

	// examine each hart
	for i := range dbg.hart {
		err := dbg.hart[i].examine()
		if err != nil {
			return nil, err
		}
	}

	return dbg, nil
}

//-----------------------------------------------------------------------------

// wrIR writes the instruction register.
func (dbg *Debug) wrIR(ir uint) error {
	if ir == dbg.ir {
		return nil
	}
	err := dbg.dev.WrIR(bitstr.FromUint(ir, dbg.irlen))
	if err != nil {
		return err
	}
	dbg.ir = ir
	return nil
}

//-----------------------------------------------------------------------------
// dtmcs

var dtmcsFields = util.FieldSet{
	{"dmihardreset", 17, 17, util.FmtDec},
	{"dmireset", 16, 16, util.FmtDec},
	{"idle", 14, 12, util.FmtDec},
	{"dmistat", 11, 10, util.FmtDec},
	{"abits", 9, 4, util.FmtDec},
	{"version", 3, 0, util.FmtDec},
}

const dmireset = (1 << 16)
const dmihardreset = (1 << 17)

// rdDtmcs reads the DTMCS register.
func (dbg *Debug) rdDtmcs() (uint, error) {
	err := dbg.wrIR(irDtmcs)
	if err != nil {
		return 0, err
	}
	tdo, err := dbg.dev.RdWrDR(bitstr.Zeros(drDtmcsLength), 0)
	if err != nil {
		return 0, err
	}
	return tdo.Split([]int{drDtmcsLength})[0], nil
}

// wrDtmcs writes the DTMCS register.
func (dbg *Debug) wrDtmcs(val uint) error {
	err := dbg.wrIR(irDtmcs)
	if err != nil {
		return err
	}
	return dbg.dev.WrDR(bitstr.FromUint(val, drDtmcsLength), 0)
}

//-----------------------------------------------------------------------------
// hart control

// GetHartCount returns the number of harts.
func (dbg *Debug) GetHartCount() int {
	return len(dbg.hart)
}

// GetHartInfo returns the hart info. id < 0 gives the current hart info.
func (dbg *Debug) GetHartInfo(id int) (*rv.HartInfo, error) {
	if id < 0 || id >= len(dbg.hart) {
		return nil, errors.New("hart id is out of range")
	}
	return &dbg.hart[id].info, nil
}

func (dbg *Debug) GetCurrentHart() *rv.HartInfo {
	return &dbg.hart[dbg.hartid].info
}

// SetCurrentHart sets the current hart.
func (dbg *Debug) SetCurrentHart(id int) (*rv.HartInfo, error) {
	if id < 0 || id >= len(dbg.hart) {
		return nil, errors.New("hart id is out of range")
	}
	err := dbg.selectHart(id)
	dbg.hartid = id
	return &dbg.hart[dbg.hartid].info, err
}

// HaltHart halts the current hart.
func (dbg *Debug) HaltHart() error {
	_, err := dbg.halt()
	halted, _ := dbg.isHalted()
	if halted {
		dbg.hart[dbg.hartid].info.State = rv.Halted
	}
	return err
}

// ResumeHart resumes the current hart.
func (dbg *Debug) ResumeHart() error {
	_, err := dbg.resume()
	running, _ := dbg.isRunning()
	if running {
		dbg.hart[dbg.hartid].info.State = rv.Running
	}
	return err
}

//-----------------------------------------------------------------------------

// Test is a test routine.
func (dbg *Debug) Test() string {
	dbg.SetCurrentHart(0)
	s := []string{}
	misa, err := dbg.RdCSR(rv.MISA)
	s = append(s, fmt.Sprintf("%08x %v", misa, err))
	return strings.Join(s, "\n")
}

//-----------------------------------------------------------------------------
