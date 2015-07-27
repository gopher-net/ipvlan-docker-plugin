package ipvlan

import "github.com/codegangsta/cli"

// Exported Flga Options
var (
	// TODO: Should the mode be passed as an additional paramter. Currently defined in the filehandle to reduce runtime arguments..
	// FlagIpVlanMode = cli.StringFlag{Name: "ipvlan-mode", Value: ipVlanMode, Usage: "name of the ipvlan mode [l2|l3]. By default, l2 mode is used"}
	FlagGateway        = cli.StringFlag{Name: "gateway", Value: gatewayIP, Usage: "(optional) IP of the default gateway"}
	FlagSubnet         = cli.StringFlag{Name: "ipvlan-subnet", Value: defaultSubnet, Usage: "subnet for the containers"}
	FlagIpvlanEthIface = cli.StringFlag{Name: "ipvlan-interface", Value: ipVlanEthIface, Usage: "physical interface that the container will be communicating on"}
)

// Unexported defaults
var (
	// TODO: Temp hardcodes, bind to CLI flags and/or dnet-ctl for bridge properties.
	ipVlanMode     = "l2"             // ipvlan l2 is the default
	ipVlanEthIface = "eth1"           // default to eth0?
	defaultSubnet  = "192.168.1.0/24" // Should this just be the eth0 IP subnet?
	gatewayIP      = ""               // GW required for L2. increment network addr+1 if not defined
)
