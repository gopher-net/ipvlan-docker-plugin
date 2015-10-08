package gobgp

import (
	"net"
)

type RibCache struct {
	BgpTable map[string]*RibLocal
}

// Unmarshalled BGP update binding for simplicity
type RibLocal struct {
	BgpPrefix    *net.IPNet
	OriginatorIP net.IP
	NextHop      net.IP
	Age          int
	Best         bool
	IsWithdraw   bool
	IsHostRoute  bool
	IsLocal      bool
	AsPath       string
}

type RibTest struct {
	BgpTable map[string]*RibLocal
}
