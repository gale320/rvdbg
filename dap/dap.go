//-----------------------------------------------------------------------------
/*

CMSIS-DAP Driver

This package implements CMSIS-DAP JTAG/SWD drivers using the hidapi library.

*/
//-----------------------------------------------------------------------------

package dap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/deadsy/hidapi"
)

//-----------------------------------------------------------------------------
// CMSIS-DAP Constants

// status codes
const (
	statusOk    = 0
	statusError = 0xff
)

// commands
const (
	cmdInfo              = 0x00 // Get Information about CMSIS-DAP Debug Unit.
	cmdHostStatus        = 0x01 // Sent status information of the debugger to Debug Unit.
	cmdConnect           = 0x02 // Connect to Device and selected DAP mode.
	cmdDisconnect        = 0x03 // Disconnect from active Debug Port.
	cmdTransferConfigure = 0x04 // Configure Transfers.
	cmdTransfer          = 0x05 // Read/write single and multiple registers.
	cmdTransferBlock     = 0x06 // Read/Write a block of data from/to a single register.
	cmdTransferAbort     = 0x07 // Abort current Transfer.
	cmdWriteAbort        = 0x08 // Write ABORT Register.
	cmdDelay             = 0x09 // Wait for specified delay.
	cmdResetTarget       = 0x0a // Reset Target with Device specific sequence.
	cmdSwjPins           = 0x10 // Control and monitor SWD/JTAG Pins.
	cmdSwjClock          = 0x11 // Select SWD/JTAG Clock.
	cmdSwjSequence       = 0x12 // Generate SWJ sequence SWDIO/TMS @SWCLK/TCK.
	cmdSwdConfigure      = 0x13 // Configure SWD Protocol.
	cmdJtagSequence      = 0x14 // Generate JTAG sequence TMS, TDI and capture TDO.
	cmdJtagConfigure     = 0x15 // Configure JTAG Chain.
	cmdJtagIDCode        = 0x16 // Read JTAG IDCODE.
	cmdSwoTransport      = 0x17 // Set SWO transport mode.
	cmdSwoMode           = 0x18 // Set SWO capture mode.
	cmdSwoBaudrate       = 0x19 // Set SWO baudrate.
	cmdSwoControl        = 0x1a // Control SWO trace data capture.
	cmdSwoStatus         = 0x1b // Read SWO trace status.
	cmdSwoData           = 0x1c // Read SWO trace data.
	cmdSwdSequence       = 0x1d // Generate SWD sequence and output on SWDIO or capture input from SWDIO data.
	cmdSwoExtendedStatus = 0x1e // Read SWO trace extended status.
	cmdQueueCommands     = 0x7e // Queue multiple DAP commands provided in a multiple packets.
	cmdExecuteCommands   = 0x7f // Execute multiple DAP commands from a single packet.
)

//-----------------------------------------------------------------------------

// Dap stores the DAP library context.
type Dap struct {
	device []*hidapi.DeviceInfo // CMSIS-DAP devices found
}

// Init initializes the DAP library.
func Init() (*Dap, error) {

	err := hidapi.Init()
	if err != nil {
		return nil, err
	}

	// get all HID devices
	hidDevice := hidapi.Enumerate(0, 0)
	if len(hidDevice) == 0 {
		hidapi.Exit()
		return nil, errors.New("no HID devices found")
	}

	// filter in the CMSIS-DAP devices
	dapDevice := []*hidapi.DeviceInfo{}
	for _, devInfo := range hidDevice {
		dev, err := hidapi.Open(devInfo.VendorID, devInfo.ProductID, "")
		if err != nil {
			continue
		}
		product, err := dev.GetProductString()
		if err != nil {
			continue
		}
		if strings.Contains(product, "CMSIS-DAP") {
			dapDevice = append(dapDevice, devInfo)
		}
		dev.Close()
	}

	dap := &Dap{
		device: dapDevice,
	}

	return dap, nil
}

// Shutdown closes the DAP library.
func (dap *Dap) Shutdown() {
	hidapi.Exit()
}

// NumDevices returns the number of devices discovered.
func (dap *Dap) NumDevices() int {
	return len(dap.device)
}

// DeviceByIndex returns DAP device information by index number.
func (dap *Dap) DeviceByIndex(idx int) (*hidapi.DeviceInfo, error) {
	if idx < 0 || idx >= len(dap.device) {
		return nil, fmt.Errorf("device index %d out of range", idx)
	}
	return dap.device[idx], nil
}

//-----------------------------------------------------------------------------
// CMSIS-DAP Device

const dapReport = 0
const usbTimeout = 500 // milliseconds

type device struct {
	hid     *hidapi.Device // HID device
	caps    capabilities   // capabilities bitmap
	pktSize int            // usb packet size
	speed   int            // lcokc speed in kHz
}

func (dev *device) String() string {
	s := []string{}
	s = append(s, fmt.Sprintf("%s", dev.hid))
	s = append(s, fmt.Sprintf("capabilities: %s", dev.caps))
	s = append(s, fmt.Sprintf("pktSize: %d bytes", dev.pktSize))
	s = append(s, fmt.Sprintf("speed: %d kHz", dev.speed))
	return strings.Join(s, "\n")
}

func newDevice(hid *hidapi.Device) *device {
	dev := &device{
		hid:     hid,
		pktSize: 64,
	}
	// get the max packet size
	maxPktSize, err := dev.getMaxPacketSize()
	if err == nil && int(maxPktSize) < dev.pktSize {
		dev.pktSize = int(maxPktSize)
	}
	// get the capabilities
	caps, _ := dev.getCapabilities()
	dev.caps = caps
	return dev
}

func (dev *device) txrx(buf []byte) ([]byte, error) {
	//fmt.Printf("tx (%d) %v\n", len(buf), buf)
	err := dev.hid.Write(buf)
	if err != nil {
		return nil, err
	}
	rx, err := dev.hid.ReadTimeout(dapReport, dev.pktSize, usbTimeout)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("rx (%d) %v\n", len(rx), rx)
	return rx, nil
}

func (dev *device) close() {
	dev.hid.Close()
}

//-----------------------------------------------------------------------------
// Connect/Disconnect Commands

const (
	modeSwd  = 1 // connect with Serial Wire Debug mode
	modeJtag = 2 // connect with 4/5-pin JTAG mode
)

// connect with the selected DAP mode
func (dev *device) connect(port byte) error {
	rx, err := dev.txrx([]byte{dapReport, cmdConnect, port})
	if err != nil {
		return err
	}
	if len(rx) < 2 || rx[0] != cmdConnect {
		return errors.New("bad response")
	}
	if rx[1] != port {
		return errors.New("connect failed")
	}
	return nil
}

// disconnect from an active debug port
func (dev *device) disconnect() error {
	rx, err := dev.txrx([]byte{dapReport, cmdDisconnect})
	if err != nil {
		return err
	}
	if len(rx) < 2 || rx[0] != cmdDisconnect {
		return errors.New("bad response")
	}
	if rx[1] != statusOk {
		return errors.New("cmdDisconnect failed")
	}
	return nil
}

//-----------------------------------------------------------------------------
// Set JTAG/SWD Clock Speed

func (dev *device) setClock(speed int) error {
	dev.speed = 0
	clk := uint32(speed * 1000)
	rx, err := dev.txrx([]byte{dapReport, cmdSwjClock, byte(clk), byte(clk >> 8), byte(clk >> 16), byte(clk >> 24)})
	if err != nil {
		return err
	}
	if len(rx) < 2 || rx[0] != cmdSwjClock {
		return errors.New("bad response")
	}
	if rx[1] != statusOk {
		return errors.New("cmdSwjClock failed")
	}
	dev.speed = speed
	return nil
}

//-----------------------------------------------------------------------------
// Pin Control

const (
	pinTCK   = 0
	pinSWCLK = 0
	pinTMS   = 1
	pinSWDIO = 1
	pinTDI   = 2
	pinTDO   = 3
	pinTRST  = 5 // active low
	pinSRST  = 7 // active low
)

func (dev *device) pinControl(pinOutput, pinSelect byte, wait uint32) (byte, error) {
	buf := []byte{
		dapReport,
		cmdSwjPins,
		pinOutput, pinSelect,
		byte(wait), byte(wait >> 8), byte(wait >> 16), byte(wait >> 24),
	}
	rx, err := dev.txrx(buf)
	if err != nil {
		return 0, err
	}
	if len(rx) < 2 || rx[0] != cmdSwjPins {
		return 0, errors.New("bad response")
	}
	return rx[1], nil
}

func (dev *device) setPins(pinOutput, pinSelect byte) error {
	_, err := dev.pinControl(pinOutput, pinSelect, 0)
	return err
}

func (dev *device) getPins() (byte, error) {
	return dev.pinControl(0, 0, 0)
}

//-----------------------------------------------------------------------------
// JTAG IDCodes

func (dev *device) getIDCode(idx byte) (uint32, error) {
	rx, err := dev.txrx([]byte{dapReport, cmdJtagIDCode, idx})
	if err != nil {
		return 0, err
	}
	if len(rx) < 6 || rx[0] != cmdJtagIDCode {
		return 0, errors.New("bad response")
	}
	if rx[1] != statusOk {
		return 0, errors.New("cmdJtagIDCode failed")
	}
	return binary.LittleEndian.Uint32(rx[2:6]), nil
}

//-----------------------------------------------------------------------------
// Control Host Status

const (
	statusConnect = 0
	statusRunning = 1
)

func boolToByte(x bool) byte {
	if x {
		return 1
	}
	return 0
}

func (dev *device) hostStatus(statusType byte, status bool) error {
	rx, err := dev.txrx([]byte{dapReport, cmdHostStatus, statusType, boolToByte(status)})
	if err != nil {
		return err
	}
	if len(rx) < 2 || rx[0] != cmdHostStatus {
		return errors.New("bad response")
	}
	if rx[1] != 0 {
		return errors.New("cmdHostStatus failed")
	}
	return nil
}

//-----------------------------------------------------------------------------
// General Commands

/*

func (dev *device) writeAbort(index byte, abort uint32) error {
	dev.txrx([]byte{dapReport, cmdWriteAbort, index, byte(abort), byte(abort >> 8), byte(abort >> 16), byte(abort >> 24)})
	return errors.New("TODO")
}

func (dev *device) delay(delay uint16) error {
	dev.txrx([]byte{dapReport, cmdDelay, byte(delay), byte(delay >> 8)})
	return errors.New("TODO")
}

func (dev *device) resetTarget() error {
	dev.txrx([]byte{dapReport, cmdResetTarget})
	return errors.New("TODO")
}

*/

//-----------------------------------------------------------------------------
// Device Information

// cmdInfo identifiers
const (
	infoVendorID        = 0x01 // Vendor ID (string)
	infoProductID       = 0x02 // Product ID (string)
	infoSerialNumber    = 0x03 // Serial Number (string)
	infoFirmwareVersion = 0x04 // Firmware Version (string)
	infoVendorName      = 0x05 // Target Device Vendor (string)
	infoDeviceName      = 0x06 // Target Device Name (string)
	infoCapabilities    = 0xf0 // Capabilities (BYTE) of the Debug Unit
	infoTestDomainTimer = 0xf1 // Test Domain Timer parameter information
	infoSwoTraceSize    = 0xfd // SWO Trace Buffer Size (WORD)
	infoMaxPacketCount  = 0xfe // maximum Packet Count (BYTE)
	infoMaxPacketSize   = 0xff // maximum Packet Size (SHORT)
)

// info gets information about CMSIS-DAP debug unit
func (dev *device) info(id byte) ([]byte, error) {
	return dev.txrx([]byte{dapReport, cmdInfo, id})
}

// getString gets a string type information item.
func (dev *device) getString(id byte) (string, error) {
	rx, err := dev.info(id)
	if err != nil {
		return "", err
	}
	if len(rx) < 2 || rx[0] != cmdInfo {
		return "", errors.New("bad response")
	}
	if rx[1] == 0 {
		return "", errors.New("no information")
	}
	n := int(rx[1])
	if len(rx) < n+2 {
		return "", errors.New("response too short")
	}
	if rx[n+1] != 0 {
		return "", errors.New("string not null terminated")
	}
	return string(rx[2 : 2+n-1]), nil
}

// getByte gets a byte type information item.
func (dev *device) getByte(id byte) (byte, error) {
	rx, err := dev.info(id)
	if err != nil {
		return 0, err
	}
	if len(rx) < 3 || rx[0] != cmdInfo || rx[1] != 1 {
		return 0, errors.New("bad response")
	}
	return rx[2], nil
}

// getShort gets a short type information item.
func (dev *device) getShort(id byte) (uint16, error) {
	rx, err := dev.info(id)
	if err != nil {
		return 0, err
	}
	if len(rx) < 4 || rx[0] != cmdInfo || rx[1] != 2 {
		return 0, errors.New("bad response")
	}
	return binary.LittleEndian.Uint16(rx[2:4]), nil
}

// getWord gets a word type information item.
func (dev *device) getWord(id byte) (uint32, error) {
	rx, err := dev.info(id)
	if err != nil {
		return 0, err
	}
	if len(rx) < 6 || rx[0] != cmdInfo || rx[1] != 4 {
		return 0, errors.New("bad response")
	}
	return binary.LittleEndian.Uint32(rx[2:6]), nil
}

// GetVendorID gets the Vendor ID
func (dev *device) getVendorID() (string, error) {
	return dev.getString(infoVendorID)
}

// GetProductID gets the Product ID
func (dev *device) getProductID() (string, error) {
	return dev.getString(infoProductID)
}

// GetSerialNumber gets the Serial Number
func (dev *device) getSerialNumber() (string, error) {
	return dev.getString(infoSerialNumber)
}

// GetFirmwareVersion gets the CMSIS-DAP Firmware Version
func (dev *device) getFirmwareVersion() (string, error) {
	return dev.getString(infoFirmwareVersion)
}

// GetVendorName gets the Target Device Vendor
func (dev *device) getVendorName() (string, error) {
	return dev.getString(infoVendorName)
}

// GetDeviceName gets the Target Device Name
func (dev *device) getDeviceName() (string, error) {
	return dev.getString(infoDeviceName)
}

// GetTestDomainTimer gets the Test Domain Timer parameter information
func (dev *device) getTestDomainTimer() (uint32, error) {
	return dev.getWord(infoTestDomainTimer)
}

// GetSwoTraceSize gets the SWO Trace Buffer Size
func (dev *device) getSwoTraceSize() (uint32, error) {
	return dev.getWord(infoSwoTraceSize)
}

// GetMaxPacketCount gets the maximum Packet Count
func (dev *device) getMaxPacketCount() (byte, error) {
	return dev.getByte(infoMaxPacketCount)
}

// GetMaxPacketSize gets the maximum Packet Size
func (dev *device) getMaxPacketSize() (uint16, error) {
	return dev.getShort(infoMaxPacketSize)
}

// capabilities is a bitmap of device capabilities
type capabilities uint

// capabilities bitmap values
const (
	capSwd              capabilities = (1 << 0) // SWD Serial Wire Debug
	capJtag             capabilities = (1 << 1) // JTAG communication
	capSwoUart          capabilities = (1 << 2) // UART Serial Wire Output
	capSwoManchester    capabilities = (1 << 3) // Manchester Serial Wire Output
	capAtomicCommand    capabilities = (1 << 4) // Atomic Commands
	capTestDomainTimer  capabilities = (1 << 5) // Test Domain Timer
	capSwoStreamTracing capabilities = (1 << 6) // SWO Streaming Trace
)

func (caps capabilities) String() string {
	s := []string{}
	if caps&capSwd != 0 {
		s = append(s, "Swd")
	}
	if caps&capJtag != 0 {
		s = append(s, "Jtag")
	}
	if caps&capSwoUart != 0 {
		s = append(s, "SwoUart")
	}
	if caps&capSwoManchester != 0 {
		s = append(s, "SwoManchester")
	}
	if caps&capAtomicCommand != 0 {
		s = append(s, "AtomicCommand")
	}
	if caps&capTestDomainTimer != 0 {
		s = append(s, "TestDomainTimer")
	}
	if caps&capSwoStreamTracing != 0 {
		s = append(s, "SwoStreamTracing")
	}
	return strings.Join(s, ",")
}

// getCapabilities gets information about the Capabilities of the Debug Unit
func (dev *device) getCapabilities() (capabilities, error) {
	rx, err := dev.info(infoCapabilities)
	if err != nil {
		return 0, err
	}
	if len(rx) < 3 || rx[0] != cmdInfo {
		return 0, errors.New("bad response")
	}
	var info0, info1 uint
	if rx[1] == 1 {
		info0 = uint(rx[2])
	}
	if len(rx) >= 4 && rx[1] == 2 {
		info1 = uint(rx[3])
	}
	return capabilities((info1 << 8) + info0), nil
}

// hasCap returns true if the device has the capability.
func (dev *device) hasCap(x capabilities) bool {
	return dev.caps&x != 0
}

//-----------------------------------------------------------------------------
