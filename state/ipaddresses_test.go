// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
)

type IPAddressSuite struct {
	ConnSuite
}

var _ = gc.Suite(&IPAddressSuite{})

func (s *IPAddressSuite) assertAddress(
	c *gc.C,
	ipAddr *state.IPAddress,
	addr network.Address,
	ipState state.AddressState,
	machineId, ifaceId, subnetId string,
) {
	c.Assert(ipAddr, gc.NotNil)
	c.Assert(ipAddr.MachineId(), gc.Equals, machineId)
	c.Assert(ipAddr.InterfaceId(), gc.Equals, ifaceId)
	c.Assert(ipAddr.SubnetId(), gc.Equals, subnetId)
	c.Assert(ipAddr.Value(), gc.Equals, addr.Value)
	c.Assert(ipAddr.Type(), gc.Equals, addr.Type)
	c.Assert(ipAddr.Scope(), gc.Equals, addr.Scope)
	c.Assert(ipAddr.State(), gc.Equals, ipState)
	c.Assert(ipAddr.Address(), jc.DeepEquals, addr)
	c.Assert(ipAddr.String(), gc.Equals, addr.String())
}

func (s *IPAddressSuite) TestAddIPAddress(c *gc.C) {
	for i, test := range []string{"0.1.2.3", "2001:db8::1"} {
		c.Logf("test %d: %q", i, test)
		addr := network.NewAddress(test, network.ScopePublic)
		ipAddr, err := s.State.AddIPAddress(addr, "foobar")
		c.Assert(err, jc.ErrorIsNil)
		s.assertAddress(c, ipAddr, addr, state.AddressStateUnknown, "", "", "foobar")

		// verify the address was stored in the state
		ipAddr, err = s.State.IPAddress(test)
		c.Assert(err, jc.ErrorIsNil)
		s.assertAddress(c, ipAddr, addr, state.AddressStateUnknown, "", "", "foobar")
	}
}

func (s *IPAddressSuite) TestAddIPAddressInvalid(c *gc.C) {
	addr := network.Address{Value: "foo"}
	_, err := s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.Satisfies, errors.IsNotValid)
	c.Assert(err, gc.ErrorMatches, `cannot add IP address "foo": address not valid`)
}

func (s *IPAddressSuite) TestAddIPAddressAlreadyExists(c *gc.C) {
	addr := network.NewAddress("0.1.2.3", network.ScopePublic)
	_, err := s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.ErrorIsNil)
	_, err = s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.Satisfies, errors.IsAlreadyExists)
	c.Assert(err, gc.ErrorMatches,
		`cannot add IP address "public:0.1.2.3": address already exists`,
	)
}

func (s *IPAddressSuite) TestIPAddressNotFound(c *gc.C) {
	_, err := s.State.IPAddress("0.1.2.3")
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
	c.Assert(err, gc.ErrorMatches, `IP address "0.1.2.3" not found`)
}

func (s *IPAddressSuite) TestRemove(c *gc.C) {
	addr := network.NewAddress("0.1.2.3", network.ScopePublic)
	ipAddr, err := s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.ErrorIsNil)

	err = ipAddr.Remove()
	c.Assert(err, jc.ErrorIsNil)

	// Doing it twice is also fine.
	err = ipAddr.Remove()
	c.Assert(err, jc.ErrorIsNil)

	_, err = s.State.IPAddress("0.1.2.3")
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
	c.Assert(err, gc.ErrorMatches, `IP address "0.1.2.3" not found`)
}

func (s *IPAddressSuite) TestAddressStateString(c *gc.C) {
	for i, test := range []struct {
		ipState state.AddressState
		expect  string
	}{{
		state.AddressStateUnknown,
		"<unknown>",
	}, {
		state.AddressStateAllocated,
		"allocated",
	}, {
		state.AddressStateUnavailable,
		"unavailable",
	}} {
		c.Logf("test %d: %q -> %q", i, test.ipState, test.expect)
		c.Check(test.ipState.String(), gc.Equals, test.expect)
	}
}

func (s *IPAddressSuite) TestSetState(c *gc.C) {
	addr := network.NewAddress("0.1.2.3", network.ScopePublic)

	for i, test := range []struct {
		initial, changeTo state.AddressState
		err               string
	}{{
		initial:  state.AddressStateUnknown,
		changeTo: state.AddressStateUnknown,
	}, {
		initial:  state.AddressStateUnknown,
		changeTo: state.AddressStateAllocated,
	}, {
		initial:  state.AddressStateUnknown,
		changeTo: state.AddressStateUnavailable,
	}, {
		initial:  state.AddressStateAllocated,
		changeTo: state.AddressStateAllocated,
	}, {
		initial:  state.AddressStateUnavailable,
		changeTo: state.AddressStateUnavailable,
	}, {
		initial:  state.AddressStateAllocated,
		changeTo: state.AddressStateUnknown,
		err: `cannot set IP address "public:0.1.2.3" to state "<unknown>": ` +
			`transition from "allocated" not valid`,
	}, {
		initial:  state.AddressStateUnavailable,
		changeTo: state.AddressStateUnknown,
		err: `cannot set IP address "public:0.1.2.3" to state "<unknown>": ` +
			`transition from "unavailable" not valid`,
	}, {
		initial:  state.AddressStateAllocated,
		changeTo: state.AddressStateUnavailable,
		err: `cannot set IP address "public:0.1.2.3" to state "unavailable": ` +
			`transition from "allocated" not valid`,
	}, {
		initial:  state.AddressStateUnavailable,
		changeTo: state.AddressStateAllocated,
		err: `cannot set IP address "public:0.1.2.3" to state "allocated": ` +
			`transition from "unavailable" not valid`,
	}} {
		c.Logf("test %d: %q -> %q ok:%v", i, test.initial, test.changeTo, test.err == "")
		ipAddr, err := s.State.AddIPAddress(addr, "foobar")
		c.Check(err, jc.ErrorIsNil)

		// Initially, all addresses have AddressStateUnknown.
		c.Assert(ipAddr.State(), gc.Equals, state.AddressStateUnknown)

		if test.initial != state.AddressStateUnknown {
			err = ipAddr.SetState(test.initial)
			c.Check(err, jc.ErrorIsNil)
		}
		err = ipAddr.SetState(test.changeTo)
		if test.err != "" {
			c.Check(err, gc.ErrorMatches, test.err)
			c.Check(err, jc.Satisfies, errors.IsNotValid)
			c.Check(ipAddr.Remove(), jc.ErrorIsNil)
			continue
		}
		c.Check(err, jc.ErrorIsNil)
		c.Check(ipAddr.State(), gc.Equals, test.changeTo)
		c.Check(ipAddr.Remove(), jc.ErrorIsNil)
	}
}

func (s *IPAddressSuite) TestAllocateTo(c *gc.C) {
	addr := network.NewAddress("0.1.2.3", network.ScopePublic)
	ipAddr, err := s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ipAddr.State(), gc.Equals, state.AddressStateUnknown)
	c.Assert(ipAddr.MachineId(), gc.Equals, "")
	c.Assert(ipAddr.InterfaceId(), gc.Equals, "")

	err = ipAddr.AllocateTo("wibble", "wobble")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ipAddr.State(), gc.Equals, state.AddressStateAllocated)
	c.Assert(ipAddr.MachineId(), gc.Equals, "wibble")
	c.Assert(ipAddr.InterfaceId(), gc.Equals, "wobble")

	freshCopy, err := s.State.IPAddress("0.1.2.3")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(freshCopy.State(), gc.Equals, state.AddressStateAllocated)
	c.Assert(freshCopy.MachineId(), gc.Equals, "wibble")
	c.Assert(freshCopy.InterfaceId(), gc.Equals, "wobble")

	// allocating twice should fail.
	err = ipAddr.AllocateTo("m", "i")
	c.Assert(err, gc.ErrorMatches,
		`cannot allocate IP address "public:0.1.2.3" to machine "m", interface "i": `+
			`already allocated or unavailable`,
	)
}

func (s *IPAddressSuite) TestAddress(c *gc.C) {
	addr := network.NewAddress("0.1.2.3", network.ScopePublic)
	ipAddr, err := s.State.AddIPAddress(addr, "foobar")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ipAddr.Address(), jc.DeepEquals, addr)

}
