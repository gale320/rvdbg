//-----------------------------------------------------------------------------
/*

RISC-V Debugger

*/
//-----------------------------------------------------------------------------

package main

import (
	"broadcom/bcm47622"
	"errors"
	"fmt"
	"os"

	cli "github.com/deadsy/go-cli"
	"github.com/deadsy/rvdbg/dap"
	"github.com/deadsy/rvdbg/jlink"
	"github.com/deadsy/rvdbg/jtag"
)

//-----------------------------------------------------------------------------

const historyPath = ".rvdbg_history"
const MHz = 1000
const mV = 1

//-----------------------------------------------------------------------------

// debugApp is state associated with the RISC-V debugger application.
type debugApp struct {
	jlinkLibrary *jlink.Jlink
	jtagDriver   *jlink.Jtag
	jtagChain    *jtag.Chain
	jtagDevice   *jtag.Device
	prompt       string
}

// newDebugApp returns a new RISC-V debugger application.
func newDebugApp() (*debugApp, error) {

	jlinkLibrary, err := jlink.Init()
	if err != nil {
		return nil, err
	}

	if jlinkLibrary.NumDevices() == 0 {
		jlinkLibrary.Shutdown()
		return nil, errors.New("no J-Link devices found")
	}

	dev, err := jlinkLibrary.DeviceByIndex(0)
	if err != nil {
		jlinkLibrary.Shutdown()
		return nil, err
	}

	jtagDriver, err := jlink.NewJtag(dev, 4*MHz, 3000*mV)
	if err != nil {
		jlinkLibrary.Shutdown()
		return nil, err
	}

	jtagChain, err := jtag.NewChain(jtagDriver, bcm47622.ChainInfo1)
	if err != nil {
		jtagDriver.Close()
		jlinkLibrary.Shutdown()
		return nil, err
	}

	jtagDevice, err := jtagChain.GetDevice(3)
	if err != nil {
		jtagDriver.Close()
		jlinkLibrary.Shutdown()
		return nil, err
	}

	return &debugApp{
		jlinkLibrary: jlinkLibrary,
		jtagDriver:   jtagDriver,
		jtagChain:    jtagChain,
		jtagDevice:   jtagDevice,
		prompt:       "rvdbg> ",
	}, nil
}

func (app *debugApp) Shutdown() {
	app.jtagDriver.Close()
	app.jlinkLibrary.Shutdown()
}

// Put outputs a string to the user application.
func (app *debugApp) Put(s string) {
	os.Stdout.WriteString(s)
}

//-----------------------------------------------------------------------------

func foo1() error {

	dapLibrary, err := dap.Init()
	if err != nil {
		return err
	}

	if dapLibrary.NumDevices() == 0 {
		dapLibrary.Shutdown()
		return errors.New("no CMSIS-DAP devices found")
	}

	devInfo, err := dapLibrary.DeviceByIndex(0)
	if err != nil {
		dapLibrary.Shutdown()
		return err
	}

	jtagDriver, err := dap.NewJtag(devInfo, 4*MHz)
	if err != nil {
		dapLibrary.Shutdown()
		return err
	}

	fmt.Printf("%s\n", jtagDriver)

	jtagDriver.Close()
	dapLibrary.Shutdown()

	return nil
}

func foo2() error {

	dapLibrary, err := dap.Init()
	if err != nil {
		return err
	}

	if dapLibrary.NumDevices() == 0 {
		dapLibrary.Shutdown()
		return errors.New("no CMSIS-DAP devices found")
	}

	devInfo, err := dapLibrary.DeviceByIndex(0)
	if err != nil {
		dapLibrary.Shutdown()
		return err
	}

	swdDriver, err := dap.NewSwd(devInfo, 4*MHz)
	if err != nil {
		dapLibrary.Shutdown()
		return err
	}

	fmt.Printf("%s\n", swdDriver)

	swdDriver.Close()
	dapLibrary.Shutdown()

	return nil
}

//-----------------------------------------------------------------------------

func main() {

	err := foo2()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	// create the application
	app, err := newDebugApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	// create the cli
	c := cli.NewCLI(app)
	c.HistoryLoad(historyPath)
	c.SetRoot(menuRoot)
	c.SetPrompt(app.prompt)

	// run the cli
	for c.Running() {
		c.Run()
	}

	// exit
	c.HistorySave(historyPath)
	app.Shutdown()
	os.Exit(0)
}

//-----------------------------------------------------------------------------
