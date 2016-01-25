package routing

import (
	log "github.com/Sirupsen/logrus"
	"github.com/gopher-net/ipvlan-docker-plugin/plugin/routing/gobgp"
	"net"
)

var routemanager RoutingInterface

type Host struct {
	isself  bool
	Address string
}

type RoutingInterface interface {
	StartMonitoring() error
	AdvertizeNewRoute(localPrefix *net.IPNet) error
	WithdrawRoute(localPrefix *net.IPNet) error
	DiscoverNew(isself bool, Address string) error
	DiscoverDelete(isself bool, Address string) error
}

func InitRoutingMonitering(masterIface string, managermode string, as string) {
	switch managermode {
	case "gobgp":
		log.Infof("Routing manager is %s", managermode)
		routemanager = gobgp.NewBgpRouteManager(masterIface, as)
	default:
		log.Infof("Default Routing manager: Gobgp")
		routemanager = gobgp.NewBgpRouteManager(masterIface, as)
	}
	error := routemanager.StartMonitoring()
	if error != nil {
		log.Fatal(error)
	}
}
func withdrawRoute(localPrefix *net.IPNet) error {
	error := routemanager.WithdrawRoute(localPrefix)
	if error != nil {
		return error
	}
	return nil
}
func AdvertizeNewRoute(localPrefix *net.IPNet) error {
	error := routemanager.AdvertizeNewRoute(localPrefix)
	if error != nil {
		return error
	}
	return nil
}
func DiscoverNew(isself bool, Address string) error {
	error := routemanager.DiscoverNew(isself, Address)
	if error != nil {
		return error
	}
	return nil
}
func DiscoverDelete(isself bool, Address string) error {
	error := routemanager.DiscoverDelete(isself, Address)
	if error != nil {
		return error
	}
	return nil
}
