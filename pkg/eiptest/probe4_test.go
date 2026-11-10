package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

func TestProbeCallStop(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()
	stop := stf.Deploy(s.Alice(), []byte{0x00})

	// CALL(10000 fixed, stop) with gas pushed LAST, then log leftover.
	c := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // argsSize
		0x60, 0x00, // argsOffset
		0x60, 0x00, // value
		0x73, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0x61, 0x27, 0x10, // PUSH2 10000
		0xf1,
		0x5a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xa0, 0x00,
	}
	copy(c[12:32], stop[:])
	addr := stf.Deploy(s.Alice(), c)
	r := stf.Tx(s.Alice(), addr, nil)
	t.Logf("CALL-STOP: status=%d gas=%d logs=%d", r.Status(), r.GasUsed(), len(r.Logs()))
	if len(r.Logs()) > 0 {
		t.Logf("leftover: %x", r.Logs()[0].Data)
	}
}
