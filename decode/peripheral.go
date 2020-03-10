//-----------------------------------------------------------------------------
/*

Peripherals

*/
//-----------------------------------------------------------------------------

package decode

//-----------------------------------------------------------------------------

// Peripheral is functionally grouped set of registers.
type Peripheral struct {
	Name  string
	Descr string
	Addr  uint
	Size  uint
	rset  RegisterSet
}

// PeripheralSet is a set of peripherals.
type PeripheralSet []Peripheral

//-----------------------------------------------------------------------------