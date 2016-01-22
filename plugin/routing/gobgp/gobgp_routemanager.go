package gobgp

import (
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"
	api "github.com/osrg/gobgp/api"
	"github.com/osrg/gobgp/packet"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"io"
	"strconv"
	"time"
)

const (
	GrcpServer = "127.0.0.1:50051"
)

type BgpRouteManager struct {
	// Master interface for IPVlan and BGP peering source
	ethIface      string
	bgpgrpcclient api.GobgpApiClient
	learnedRoutes []RibLocal
	asnum         int
	ModPathCh     chan *api.Path
	ModPeerCh     chan *api.ModNeighborArguments
}

func NewBgpRouteManager(masterIface string, as string) *BgpRouteManager {
	a, err := strconv.Atoi(as)
	if err != nil {
		log.Errorf("AS number must be only numeral %s, using default AS num: 65000", as)
		a = 65000
	}
	b := &BgpRouteManager{
		ethIface:  masterIface,
		asnum:     a,
		ModPathCh: make(chan *api.Path),
		ModPeerCh: make(chan *api.ModNeighborArguments),
	}
	return b
}
func (b *BgpRouteManager) SetBgpConfig(RouterId string) error {
	_, err := b.bgpgrpcclient.ModGlobalConfig(context.Background(), &api.ModGlobalConfigArguments{
		Operation: api.Operation_ADD,
		Global: &api.Global{
			As:       uint32(b.asnum),
			RouterId: RouterId,
		},
	})
	if err != nil {
		return err
	}
	log.Debugf("Set BGP Global config: as %d, router id %v", b.asnum, RouterId)
	return nil
}

func (b *BgpRouteManager) StartMonitoring() error {
	err := cleanExistingRoutes(b.ethIface)
	if err != nil {
		log.Infof("Error cleaning old routes: %s", err)
	}
	bgpCache := &RibCache{
		BgpTable: make(map[string]*RibLocal),
	}
	timeout := grpc.WithTimeout(time.Second)
	conn, err := grpc.Dial(GrcpServer, timeout, grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	b.bgpgrpcclient = api.NewGobgpApiClient(conn)
	RibCh := make(chan *api.Path)
	go b.monitorBestPath(RibCh)
	log.Info("Initialization complete, now monitoring BGP for new routes..")
	for {
		select {
		case p := <-RibCh:
			monitorUpdate, err := bgpCache.handleBgpRibMonitor(p)

			if err != nil {
				log.Errorf("error processing bgp update [ %s ]", err)
			}
			if monitorUpdate.IsLocal != true {
				if p.IsWithdraw {
					monitorUpdate.IsWithdraw = true
					log.Infof("BGP update has [ withdrawn ] the IP prefix [ %s ]", monitorUpdate.BgpPrefix.String())
					// If the bgp update contained a withdraw, remove the local netlink route for the remote endpoint
					err = delNetlinkRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, b.ethIface)
					if err != nil {
						log.Errorf("Error removing learned bgp route [ %s ]", err)
					}
				} else {
					monitorUpdate.IsWithdraw = false
					b.learnedRoutes = append(b.learnedRoutes, *monitorUpdate)
					log.Debugf("Learned routes: %v ", monitorUpdate)

					err = addNetlinkRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, b.ethIface)
					if err != nil {
						log.Debugf("Error Adding route results [ %s ]", err)
					}

					log.Infof("Updated the local prefix cache from the newly learned BGP update:")
					for n, entry := range b.learnedRoutes {
						log.Debugf("%d - %+v", n+1, entry)
					}
				}
			}
			log.Debugf("Verbose update details: %s", monitorUpdate)

		case arg := <-b.ModPeerCh:
			_, err := b.bgpgrpcclient.ModNeighbor(context.Background(), arg)
			log.Debugf("Mod Peer with grpc %v", arg)
			if err != nil {
				return err
			}

		case path := <-b.ModPathCh:
			n, _ := bgp.NewPathAttributeNextHop("0.0.0.0").Serialize()
			path.Pattrs = append(path.Pattrs, n)
			origin, _ := bgp.NewPathAttributeOrigin(bgp.BGP_ORIGIN_ATTR_TYPE_IGP).Serialize()
			path.Pattrs = append(path.Pattrs, origin)
			arg := &api.ModPathArguments{
				Operation: api.Operation_ADD,
				Resource:  api.Resource_GLOBAL,
				Name:      "",
				Path:      path,
			}
			log.Debugf("Mod Path with grcp %v", arg)
			_, err := b.bgpgrpcclient.ModPath(context.Background(), arg)
			if err != nil {
				return err
			}
		}
	}
}

func (b *BgpRouteManager) monitorBestPath(RibCh chan *api.Path) error {
	timeout := grpc.WithTimeout(time.Second)
	conn, err := grpc.Dial(GrcpServer, timeout, grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := api.NewGobgpApiClient(conn)

	table := &api.Table{
		Type:   api.Resource_GLOBAL,
		Family: uint32(bgp.RF_IPv4_UC),
		Name:   "",
	}
	arg := &api.Arguments{
		Resource: api.Resource_GLOBAL,
		Family:   uint32(bgp.RF_IPv4_UC),
	}

	err = func() error {
		rib, err := client.GetRib(context.Background(), table)
		if err != nil {
			return err
		}
		for _, d := range rib.Destinations {
			for _, p := range d.Paths {
				if p.Best {
					RibCh <- p
					break
				}
			}
		}
		return nil
	}()

	if err != nil {
		return err
	}
	stream, err := client.MonitorBestChanged(context.Background(), arg)
	if err != nil {
		return err
	}
	for {
		dst, err := stream.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		RibCh <- dst.Paths[0]
	}
	return nil
}

// Advertise the local namespace IP prefixes to the bgp neighbors
func (b *BgpRouteManager) AdvertizeNewRoute(localPrefix *net.IPNet) error {
	log.Infof("Adding this hosts container network [ %s ] into the BGP domain", localPrefix)
	path := &api.Path{
		Pattrs:     make([][]byte, 0),
		IsWithdraw: false,
	}
	localPrefixMask, _ := localPrefix.Mask.Size()
	path.Nlri, _ = bgp.NewIPAddrPrefix(uint8(localPrefixMask), localPrefix.IP.String()).Serialize()
	b.ModPathCh <- path
	return nil
}

func (b *BgpRouteManager) WithdrawRoute(localPrefix *net.IPNet) error {
	log.Infof("Withdraw this hosts container network [ %s ] from the BGP domain", localPrefix)
	path := &api.Path{
		Pattrs:     make([][]byte, 0),
		IsWithdraw: true,
	}
	localPrefixMask, _ := localPrefix.Mask.Size()
	path.Nlri, _ = bgp.NewIPAddrPrefix(uint8(localPrefixMask), localPrefix.IP.String()).Serialize()
	b.ModPathCh <- path
	return nil
}

func (b *BgpRouteManager) ModPeer(peeraddr string, operation api.Operation) error {
	peer := &api.Peer{
		Conf: &api.PeerConf{
			NeighborAddress: peeraddr,
			PeerAs:          uint32(b.asnum),
		},
	}
	arg := &api.ModNeighborArguments{
		Operation: operation,
		Peer:      peer,
	}
	log.Debugf("Mod peer arg: %v", arg)
	b.ModPeerCh <- arg
	return nil
}

func (b *BgpRouteManager) DiscoverNew(isself bool, Address string) error {
	if isself {
		error := b.SetBgpConfig(Address)
		if error != nil {
			return error
		}
	} else {
		log.Debugf("BGP neighbor add %s", Address)
		error := b.ModPeer(Address, api.Operation_ADD)
		if error != nil {
			return error
		}
	}
	return nil
}

func (b *BgpRouteManager) DiscoverDelete(isself bool, Address string) error {
	if isself {
		return nil
	} else {
		log.Debugf("BGP neighbor del %s", Address)
		error := b.ModPeer(Address, api.Operation_DEL)
		if error != nil {
			return error
		}
	}
	return nil
}
func (cache *RibCache) handleBgpRibMonitor(routeMonitor *api.Path) (*RibLocal, error) {
	ribLocal := &RibLocal{}
	var nlri bgp.AddrPrefixInterface

	if len(routeMonitor.Nlri) > 0 {
		nlri = &bgp.IPAddrPrefix{}
		err := nlri.DecodeFromBytes(routeMonitor.Nlri)
		if err != nil {
			log.Errorf("Error parsing the bgp update nlri")
		}
		bgpPrefix, err := ParseIPNet(nlri.String())
		if err != nil {
			log.Errorf("Error parsing the bgp update prefix")
		}
		ribLocal.BgpPrefix = bgpPrefix

	}
	log.Debugf("BGP update for prefix: [ %s ] ", nlri.String())
	for _, attr := range routeMonitor.Pattrs {
		p, err := bgp.GetPathAttribute(attr)
		if err != nil {
			log.Errorf("Error parsing the bgp update attr")
		}
		err = p.DecodeFromBytes(attr)
		if err != nil {
			log.Errorf("Error parsing the bgp update attr")
		}
		log.Debugf("Type: [ %d ] ,Value [ %s ]", p.GetType(), p.String())
		switch p.GetType() {
		case bgp.BGP_ATTR_TYPE_ORIGIN:
			// 0 = iBGP; 1 = eBGP
			if p.(*bgp.PathAttributeOrigin).Value != nil {
				log.Debugf("Type Code: [ %d ] Origin: %g", bgp.BGP_ATTR_TYPE_ORIGIN, p.(*bgp.PathAttributeOrigin).String())
			}
		case bgp.BGP_ATTR_TYPE_AS_PATH:
			if p.(*bgp.PathAttributeAsPath).Value != nil {
				log.Debugf("Type Code: [ %d ] AS_Path: %s", bgp.BGP_ATTR_TYPE_AS_PATH, p.String())
			}
		case bgp.BGP_ATTR_TYPE_NEXT_HOP:
			if p.(*bgp.PathAttributeNextHop).Value.String() != "" {
				log.Debugf("Type Code: [ %d ] Nexthop: %s", bgp.BGP_ATTR_TYPE_NEXT_HOP, p.String())
				n := p.(*bgp.PathAttributeNextHop)
				ribLocal.NextHop = n.Value
				if ribLocal.NextHop.String() == "0.0.0.0" {
					ribLocal.IsLocal = true
				}
			}
		case bgp.BGP_ATTR_TYPE_MULTI_EXIT_DISC:
			if p.(*bgp.PathAttributeMultiExitDisc).Value >= 0 {
				log.Debugf("Type Code: [ %d ] MED: %g", bgp.BGP_ATTR_TYPE_MULTI_EXIT_DISC, p.String())
			}
		case bgp.BGP_ATTR_TYPE_LOCAL_PREF:
			if p.(*bgp.PathAttributeLocalPref).Value >= 0 {
				log.Debugf("Type Code: [ %d ] Local Pref: %g", bgp.BGP_ATTR_TYPE_LOCAL_PREF, p.String())
			}
		case bgp.BGP_ATTR_TYPE_ORIGINATOR_ID:
			if p.(*bgp.PathAttributeOriginatorId).Value != nil {
				log.Debugf("Type Code: [ %d ] Originator IP: %s", bgp.BGP_ATTR_TYPE_ORIGINATOR_ID, p.String())
				ribLocal.OriginatorIP = p.(*bgp.PathAttributeOriginatorId).Value
				log.Debugf("Type Code: [ %d ] Originator IP: %s", bgp.BGP_ATTR_TYPE_ORIGINATOR_ID, ribLocal.OriginatorIP)
			}
		case bgp.BGP_ATTR_TYPE_CLUSTER_LIST:
			if len(p.(*bgp.PathAttributeClusterList).Value) > 0 {
				log.Debugf("Type Code: [ %d ] Cluster List: %s", bgp.BGP_ATTR_TYPE_CLUSTER_LIST, p.String())
			}
		case bgp.BGP_ATTR_TYPE_MP_REACH_NLRI:
			if p.(*bgp.PathAttributeMpReachNLRI).Value != nil {
				log.Debugf("Type Code: [ %d ] MP Reachable: %v", bgp.BGP_ATTR_TYPE_MP_REACH_NLRI, p.String())
				mpreach := p.(*bgp.PathAttributeMpReachNLRI)
				if len(mpreach.Value) != 1 {
					log.Errorf("include only one route in mp_reach_nlri")
				}
				nlri = mpreach.Value[0]
				ribLocal.NextHop = mpreach.Nexthop
				if ribLocal.NextHop.String() == "0.0.0.0" {
					ribLocal.IsLocal = true
				}
			}
		case bgp.BGP_ATTR_TYPE_MP_UNREACH_NLRI:
			if p.(*bgp.PathAttributeMpUnreachNLRI).Value != nil {
				log.Debugf("Type Code: [ %d ]  MP Unreachable: %v", bgp.BGP_ATTR_TYPE_MP_UNREACH_NLRI, p.String())
			}
		case bgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES:
			if p.(*bgp.PathAttributeExtendedCommunities).Value != nil {
				log.Debugf("Type Code: [ %d ] Extended Communities: %v", bgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, p.String())
			}
		default:
			log.Errorf("Unknown BGP attribute code [ %d ]")
		}
	}
	return ribLocal, nil

}

// return string representation of pluginConfig for debugging
func (d *RibLocal) stringer() string {
	str := fmt.Sprintf("Prefix:[ %s ], ", d.BgpPrefix.String())
	str = str + fmt.Sprintf("OriginatingIP:[ %s ], ", d.OriginatorIP.String())
	str = str + fmt.Sprintf("Nexthop:[ %s ], ", d.NextHop.String())
	str = str + fmt.Sprintf("IsWithdrawn:[ %t ], ", d.IsWithdraw)
	str = str + fmt.Sprintf("IsHostRoute:[ %t ]", d.IsHostRoute)
	return str
}

func ParseIPNet(s string) (*net.IPNet, error) {
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return nil, err
	}
	return &net.IPNet{IP: ip, Mask: ipNet.Mask}, nil
}
