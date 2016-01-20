package ipvlan

import (
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func makeMac(ip net.IP) string {
	hw := make(net.HardwareAddr, 6)
	hw[0] = 0x7a
	hw[1] = 0x42
	copy(hw[2:], ip.To4())
	return hw.String()
}

// Return the IPv4 address of a network interface
func getIfaceAddr(name string) (*net.IPNet, error) {
	iface, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}
	addrs, err := netlink.AddrList(iface, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("Interface %s has no IP addresses", name)
	}
	if len(addrs) > 1 {
		log.Infof("Interface [ %v ] has more than 1 IPv4 address. Defaulting to using [ %v ]\n", name, addrs[0].IP)
	}
	return addrs[0].IPNet, nil
}

// Set the IP addr of a link
func (driver *driver) setInterfaceIP(name string, rawIP string) error {
	iface, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	ipNet, err := netlink.ParseIPNet(rawIP)
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: ipNet, Label: ""}
	return netlink.AddrAdd(iface, addr)
}

// Increment an an octet
func ipIncrement(networkAddr net.IP) net.IP {
	for i := 15; i >= 0; i-- {
		o := networkAddr[i]
		if o < 255 {
			networkAddr[i] = o + 1
			for rng := i + 1; rng <= 15; rng++ {
				networkAddr[rng] = 0
			}
			break
		}
	}
	return networkAddr
}

func getIPVlanMode(s string) (netlink.IPVlanMode, error) {
	switch s {
	case "", "l2":
		return netlink.IPVLAN_MODE_L2, nil
	case "l3", "l3routing":
		return netlink.IPVLAN_MODE_L3, nil
	default:
		return 0, fmt.Errorf("unknown ipvlan mode: %q", s)
	}
}

// Check if a netlink interface exists in the default namespace
func validateHostIface(ifaceStr string) bool {
	_, err := net.InterfaceByName(ifaceStr)
	if err != nil {
		log.Debugf("The requested interface to delete [ %s ] was not found on the host: %s", ifaceStr, err)
		return false
	}
	return true
}
