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
	"time"
)

type BgpRouteManager struct {
	// Master interface for IPVlan and BGP peering source
	ethIface      string
	grpcserver    net.IP
	bgpgrpcclient api.GobgpApiClient
	learnedRoutes []RibLocal
}

func NewBgpRouteManager(masterIface string, server net.IP) *BgpRouteManager {
	ret := &BgpRouteManager{
		ethIface:   masterIface,
		grpcserver: server,
	}
	return ret
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
	conn, err := grpc.Dial("127.0.0.1:8080", timeout, grpc.WithBlock(), grpc.WithInsecure())
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
						log.Debugf("Add route results [ %s ]", err)
					}

					log.Infof("Updated the local prefix cache from the newly learned BGP update:")
					for n, entry := range b.learnedRoutes {
						log.Debugf("%d - %+v", n+1, entry)
					}
				}
			}
			log.Debugf("Verbose update details: %s", monitorUpdate)
		}
	}
}

func (b *BgpRouteManager) monitorBestPath(RibCh chan *api.Path) error {
	timeout := grpc.WithTimeout(time.Second)
	conn, err := grpc.Dial("127.0.0.1:8080", timeout, grpc.WithBlock(), grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := api.NewGobgpApiClient(conn)

	arg := &api.Arguments{
		Resource: api.Resource_GLOBAL,
		Rf:       uint32(bgp.RF_IPv4_UC),
	}
	err = func() error {
		stream, err := client.GetRib(context.Background(), arg)
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
			for _, p := range dst.Paths {
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
	n, _ := bgp.NewPathAttributeNextHop("0.0.0.0").Serialize()
	path.Pattrs = append(path.Pattrs, n)
	origin, _ := bgp.NewPathAttributeOrigin(bgp.BGP_ORIGIN_ATTR_TYPE_IGP).Serialize()
	path.Pattrs = append(path.Pattrs, origin)
	arg := &api.ModPathArguments{
		Resource: api.Resource_GLOBAL,
		Paths:    []*api.Path{path},
	}
	stream, err := b.bgpgrpcclient.ModPath(context.Background())
	if err != nil {
		return err
	}

	err = stream.Send(arg)
	if err != nil {
		return err
	}
	stream.CloseSend()

	res, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	if res.Code != api.Error_SUCCESS {
		return fmt.Errorf("error: code: %d, msg: %s\n", res.Code, res.Msg)
	}
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
	n, _ := bgp.NewPathAttributeNextHop("0.0.0.0").Serialize()
	path.Pattrs = append(path.Pattrs, n)
	origin, _ := bgp.NewPathAttributeOrigin(bgp.BGP_ORIGIN_ATTR_TYPE_IGP).Serialize()
	path.Pattrs = append(path.Pattrs, origin)
	arg := &api.ModPathArguments{
		Resource: api.Resource_GLOBAL,
		Paths:    []*api.Path{path},
	}
	stream, err := b.bgpgrpcclient.ModPath(context.Background())
	if err != nil {
		return err
	}

	err = stream.Send(arg)
	if err != nil {
		return err
	}
	stream.CloseSend()

	res, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	if res.Code != api.Error_SUCCESS {
		return fmt.Errorf("error: code: %d, msg: %s\n", res.Code, res.Msg)
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
