package ipvlan

import "github.com/codegangsta/cli"

var (
	// Exported user CLI flag config options
	// Most of these are depricated with libnetwork now accepting --options
	FlagIPVlanMode     = cli.StringFlag{Name: "mode", Value: ipVlanMode, Usage: "name of the ipvlan mode [l2|l3]. (default: l2)"}
	FlagGateway        = cli.StringFlag{Name: "gateway", Value: "", Usage: "IP of the default gateway (defaultL2 mode: first usable address of a subnet. Subnet 192.168.1.0/24 would mean the container gateway to 192.168.1.1)"}
	FlagSubnet         = cli.StringFlag{Name: "ipvlan-subnet", Value: defaultSubnet, Usage: "subnet for the containers (l2 mode: 192.168.1.0/24)"}
	FlagMtu            = cli.IntFlag{Name: "mtu", Value: cliMTU, Usage: "MTU of the container interface (default: 1500)"}
	FlagIpvlanEthIface = cli.StringFlag{Name: "host-interface", Value: ipVlanEthIface, Usage: "(required) interface that the container will be communicating outside of the docker host with"}
	FlagRoutingManager = cli.StringFlag{Name: "routemng", Value: routingManager, Usage: "name of the routing manager name [gobgp]. (default: gobgp)"}
	FlagBgpAs          = cli.StringFlag{Name: "as", Value: BgpAs, Usage: "AS number of bgp router. (default: 65000)"}
)

var (
	// These are the default values that are overwritten if flags are used at runtime
	ipVlanMode     = "l2"             // ipvlan l2 is the default
	ipVlanEthIface = "eth1"           // default to eth0?
	defaultSubnet  = "192.168.1.0/24" // Should this just be the eth0 IP subnet?
	cliMTU         = 1500
	routingManager = "gobgp"
	BgpAs          = "65000"
)
