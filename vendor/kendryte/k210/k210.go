//-----------------------------------------------------------------------------
/*

k210
See:

*/
//-----------------------------------------------------------------------------

package k210

import "github.com/deadsy/rvdbg/jtag"

//-----------------------------------------------------------------------------
/*

K210 JTAG Layout

*/

var ChainInfo = []jtag.DeviceInfo{
	// irlen, idcode, name
	jtag.DeviceInfo{5, jtag.IDCode(0), "k210-rv64"},
}

//-----------------------------------------------------------------------------
