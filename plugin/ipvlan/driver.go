package ipvlan

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/docker/libnetwork/ipallocator"
	"github.com/docker/libnetwork/iptables"
	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
	"github.com/vishvananda/netlink"
	"github.com/docker/libnetwork/types"
)

const (
	MethodReceiver     = "NetworkDriver"
	defaultRoute       = "0.0.0.0/0"
	containerEthPrefix = "eth"
	ipVlanL2           = "l2"
	ipVlanL3           = "l3"
	minMTU             = 68
)

type ipvlanType netlink.IPVlanMode

const (
	_ ipvlanType = iota
	IPVLAN_MODE_L2
	IPVLAN_MODE_L3
)

type Driver interface {
	Listen(string) error
}

type driver struct {
	dockerer
	ipAllocator *ipallocator.IPAllocator
	version     string
	network     string
	cidr        *net.IPNet
	nameserver  string
	pluginConfig
}

// Struct for binding plugin specific configurations (cli.go for details).
type pluginConfig struct {
	mtu             int
	mode            string
	hostIface       string
	containerSubnet *net.IPNet
	gatewayIP       net.IP
}

func New(version string, ctx *cli.Context) (Driver, error) {
	docker, err := dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)
	if err != nil {
		return nil, fmt.Errorf("could not connect to docker: %s", err)
	}
	// bind CLI opts to the user config struct
	if ok := validateHostIface(ctx.String("host-interface")); !ok {
		log.Fatalf("Requird field [ host-interface ] ethernet interface [ %s ] was not found. Exiting since this is required for both l2 and l3 modes.", ctx.String("host-interface"))
	}
	ipVlanEthIface = ctx.String("host-interface")

	// lower bound of v4 MTU is 68-bytes per rfc791
	if ctx.Int("mtu") >= minMTU {
		defaultMTU = ctx.Int("mtu")
	} else {
		log.Fatalf("The MTU value passed [ %d ] must be greater then [ %d ] bytes per rfc791", ctx.Int("mtu"), minMTU)
	}

	// Parse the container IP subnet
	containerGW, cidr, err := net.ParseCIDR(ctx.String("ipvlan-subnet"))
	if err != nil {
		log.Fatalf("Error parsing cidr from the subnet flag provided [ %s ]: %s", FlagSubnet, err)
	}

	// For ipvlan L2 mode a gateway IP address is used just like any other
	// normal L2 domain. If no gateway is specified, we attempt to guess using
	// the first usable IP on the container subnet from the CLI argument or from
	// the defaultSubnet "192.168.1.0/24" which results in a gatway of "192.168.1.1".
	switch ctx.String("mode") {
	case ipVlanL2:
		ipVlanMode = ipVlanL2
		if ctx.String("gateway") != "" {
			// bind the container gateway to the IP passed from the CLI
			cliGateway, _, err := net.ParseCIDR(ctx.String("gateway"))
			if err != nil {
				log.Fatalf("The IP passed with the [ gateway ] flag [ %s ] was not a valid address: %s", FlagGateway.Value, err)
			}
			containerGW = cliGateway
		} else {
			// if no gateway was passed, guess the first valid address on the container subnet
			containerGW = ipIncrement(containerGW)
		}
	case ipVlanL3:
		// IPVlan simply needs the container interface for its
		// default route target since only unicast is allowed <3
		ipVlanMode = ipVlanL3
		containerGW = nil
	default:
		log.Fatalf("Invalid IPVlan mode supplied [ %s ]. IPVlan has two modes: [ %s ] or [%s ]", ctx.String("mode"), ipVlanL2, ipVlanL3)
	}

	pluginOpts := &pluginConfig{
		mtu:             defaultMTU,
		mode:            ipVlanMode,
		containerSubnet: cidr,
		gatewayIP:       containerGW,
		hostIface:       ipVlanEthIface,
	}
	log.Debugf("Plugin configuration options are: \n %s", pluginOpts)

	ipAllocator := ipallocator.New()
	d := &driver{
		dockerer: dockerer{
			client: docker,
		},
		ipAllocator:  ipAllocator,
		version:      version,
		pluginConfig: *pluginOpts,
	}
	return d, nil
}

func (driver *driver) Listen(socket string) error {
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(notFound)

	router.Methods("GET").Path("/status").HandlerFunc(driver.status)
	router.Methods("POST").Path("/Plugin.Activate").HandlerFunc(driver.handshake)

	handleMethod := func(method string, h http.HandlerFunc) {
		router.Methods("POST").Path(fmt.Sprintf("/%s.%s", MethodReceiver, method)).HandlerFunc(h)
	}
	handleMethod("CreateNetwork", driver.createNetwork)
	handleMethod("DeleteNetwork", driver.deleteNetwork)
	handleMethod("CreateEndpoint", driver.createEndpoint)
	handleMethod("DeleteEndpoint", driver.deleteEndpoint)
	handleMethod("EndpointOperInfo", driver.infoEndpoint)
	handleMethod("Join", driver.joinEndpoint)
	handleMethod("Leave", driver.leaveEndpoint)
	var (
		listener net.Listener
		err      error
	)

	listener, err = net.Listen("unix", socket)
	if err != nil {
		return err
	}

	return http.Serve(listener, router)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	log.Warnf("[plugin] Not found: %+v", r)
	http.NotFound(w, r)
}

func sendError(w http.ResponseWriter, msg string, code int) {
	log.Errorf("%d %s", code, msg)
	http.Error(w, msg, code)
}

func errorResponsef(w http.ResponseWriter, fmtString string, item ...interface{}) {
	json.NewEncoder(w).Encode(map[string]string{
		"Err": fmt.Sprintf(fmtString, item...),
	})
}

func objectResponse(w http.ResponseWriter, obj interface{}) {
	if err := json.NewEncoder(w).Encode(obj); err != nil {
		sendError(w, "Could not JSON encode response", http.StatusInternalServerError)
		return
	}
}

func emptyResponse(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]string{})
}

type handshakeResp struct {
	Implements []string
}

func (driver *driver) handshake(w http.ResponseWriter, r *http.Request) {
	err := json.NewEncoder(w).Encode(&handshakeResp{
		[]string{"NetworkDriver"},
	})
	if err != nil {
		log.Fatalf("handshake encode: %s", err)
		sendError(w, "encode error", http.StatusInternalServerError)
		return
	}
	log.Debug("Handshake completed")
}

func (driver *driver) status(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintln("ipvlan plugin", driver.version))
}

type networkCreate struct {
	NetworkID string
	Options   map[string]interface{}
}

func (driver *driver) createNetwork(w http.ResponseWriter, r *http.Request) {
	var create networkCreate
	err := json.NewDecoder(r.Body).Decode(&create)
	if err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if driver.network != "" {
		errorResponsef(w, "You get just one network, and you already made %s", driver.network)
		return
	}
	driver.network = create.NetworkID

	cidr := driver.pluginConfig.containerSubnet
	driver.cidr = cidr
	// Todo: check for ipam errors
	driver.ipAllocator.RequestIP(cidr, nil)

	emptyResponse(w)

	err = driver.natOut()
	if err != nil {
		log.Warnf("error setting up outboud nat: %s", err)
	}
}

type networkDelete struct {
	NetworkID string
}

func (driver *driver) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	var delete networkDelete
	if err := json.NewDecoder(r.Body).Decode(&delete); err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Debugf("Delete network request: %+v", &delete)
	if delete.NetworkID != driver.network {
		log.Debugf("network not found: %+v", &delete)
		errorResponsef(w, "Network %s not found", delete.NetworkID)
		return
	}
	driver.network = ""
	emptyResponse(w)
	log.Infof("Destroy network %s", delete.NetworkID)
}

type endpointCreate struct {
	NetworkID  string
	EndpointID string
	Interfaces []*iface
	Options    map[string]interface{}
}

type iface struct {
	ID         int
	SrcName    string
	DstPrefix  string
	Address    string
	MacAddress string
}

type endpointResponse struct {
	Interfaces []*iface
}

func (driver *driver) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var create endpointCreate
	if err := json.NewDecoder(r.Body).Decode(&create); err != nil {
		sendError(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	netID := create.NetworkID
	endID := create.EndpointID

	if netID != driver.network {
		log.Warnf("Network not found, [ %s ]", netID)
		errorResponsef(w, "No such network %s", netID)
		return
	}
	log.Debugf("The container subnet for this context is [ %s ]", driver.pluginConfig.containerSubnet.String())
	// Request an IP address from libnetwork based on the cidr scope
	// TODO: Add a user defined static ip addr option
	allocatedIP, err := driver.ipAllocator.RequestIP(driver.pluginConfig.containerSubnet, nil)
	if err != nil || allocatedIP == nil {
		log.Errorf("Unable to obtain an IP address from libnetwork ipam: %s", err)
		errorResponsef(w, "%s", err)
		return
	}
	// generate a mac address for the pending container
	mac := makeMac(allocatedIP)
	// Have to convert container IP to a string ip/mask format
	bridgeMask := strings.Split(driver.pluginConfig.containerSubnet.String(), "/")
	containerAddress := allocatedIP.String() + "/" + bridgeMask[1]

	log.Infof("Allocated container IP: [ %s ]", containerAddress)
	
	respIface := &iface{
		Address:    containerAddress,
		MacAddress: mac,
	}
	resp := &endpointResponse{
		Interfaces: []*iface{respIface},
	}
	objectResponse(w, resp)
	log.Debugf("Create endpoint %s %+v", endID, resp)
}

type endpointDelete struct {
	NetworkID  string
	EndpointID string
}

func (driver *driver) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	var delete endpointDelete
	if err := json.NewDecoder(r.Body).Decode(&delete); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Debugf("Delete endpoint request: %+v", &delete)
	emptyResponse(w)
	// null check cidr in case driver restarted and doesnt know the network to avoid panic
	if driver.cidr == nil {
		return
	}
	// ReleaseIP releases an ip back to a network
	if err := driver.ipAllocator.ReleaseIP(driver.cidr, driver.cidr.IP); err != nil {
		log.Warnf("error releasing IP: %s", err)
	}
	log.Debugf("Delete endpoint %s", delete.EndpointID)

	containerLink := delete.EndpointID[:5]

	log.Infof("Removing unused link [ %s ]", containerLink)
	clink, err := netlink.LinkByName(containerLink)
	if err != nil {
		log.Warnf("Error looking up link [ %s ] object: [ %v ] error: [ %s ]", clink.Attrs().Name, clink, err)
	}
	if err := netlink.LinkDel(clink); err != nil {
		log.Errorf("unable to delete the container's link [ %s ] on leave: %s", clink, err)
	}
}

type endpointInfoReq struct {
	NetworkID  string
	EndpointID string
}

type endpointInfo struct {
	Value map[string]interface{}
}

func (driver *driver) infoEndpoint(w http.ResponseWriter, r *http.Request) {
	var info endpointInfoReq
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Debugf("Endpoint info request: %+v", &info)
	objectResponse(w, &endpointInfo{Value: map[string]interface{}{}})
	log.Debugf("Endpoint info %s", info.EndpointID)
}

type joinInfo struct {
	InterfaceNames []*iface
	Gateway        string
	GatewayIPv6    string
	HostsPath      string
	ResolvConfPath string
}

type join struct {
	NetworkID  string
	EndpointID string
	SandboxKey string
	Options    map[string]interface{}
}

type staticRoute struct {
	Destination string
	RouteType   int
	NextHop     string
	InterfaceID int
}

type joinResponse struct {
	HostsPath      string
	ResolvConfPath string
	Gateway        string
	InterfaceNames []*iface
	StaticRoutes   []*staticRoute
}

func (driver *driver) joinEndpoint(w http.ResponseWriter, r *http.Request) {
	var j join
	if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Debugf("Join request: %+v", &j)

	endID := j.EndpointID
	// unique name while still on the common netns
	preMoveName := endID[:5]
	mode, err := getIpVlanMode(ipVlanMode)
	if err != nil {
		log.Errorf("error getting vlan mode [ %v ]: %s", mode, err)
		return
	}
	m, err := netlink.LinkByName(ipVlanEthIface)
	if err != nil {
		log.Warnf("Error looking up the parent iface [ %s ] error: [ %s ]", ipVlanEthIface, err)
	}
	ipvlan := &netlink.IPVlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        preMoveName,
			ParentIndex: m.Attrs().Index,
		},
		Mode: mode,
	}
	if err := netlink.LinkAdd(ipvlan); err != nil {
		log.Warnf("failed to create the netlink link: [ %v ] with the error: %s", ipvlan, err)
	}
	log.Infof("Created IPVlan port of: [ %s ] and mode: [ %s ]", ipvlan.Name, ipVlanMode)

//	if err := netlink.LinkSetMTU(ipvlan, defaultMTU); err != nil {
//		log.Errorf("Error setting the MTU [ %d ] for link [ %s ]: %s", defaultMTU, ipvlan.Name, err)
//	}
	// SrcName gets renamed to DstPrefix on the container iface
	ifname := &iface{
		SrcName:   ipvlan.Name,
		DstPrefix: containerEthPrefix,
		ID:        0,
	}
	res := &joinResponse{}

	// L2 ipvlan needs an explicit IP for a default GW in the container netns
	if ipVlanMode == ipVlanL2 {
		res = &joinResponse{
			InterfaceNames: []*iface{ifname},
			Gateway:        driver.pluginConfig.gatewayIP.String(),
		}
		defer objectResponse(w, res)
	}

	// ipvlan L3 mode doesnt need an IP for a default GW, just an iface dex.
	if ipVlanMode == ipVlanL3 {
		res = &joinResponse{
			InterfaceNames: []*iface{ifname},
		}
		// Add a default route of only the interface inside the container
		defaultRoute := &staticRoute{
			Destination: defaultRoute,
			RouteType:   types.CONNECTED,
			NextHop:     "",
			InterfaceID: 0,
		}
		res.StaticRoutes = []*staticRoute{defaultRoute}
	}

	// Send the response to libnetwork
	objectResponse(w, res)
	log.Debugf("Join endpoint %s:%s to %s", j.NetworkID, j.EndpointID, j.SandboxKey)
}

type leave struct {
	NetworkID  string
	EndpointID string
	Options    map[string]interface{}
}

func (driver *driver) leaveEndpoint(w http.ResponseWriter, r *http.Request) {
	var l leave
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		sendError(w, "Could not decode JSON encode payload", http.StatusBadRequest)
		return
	}
	log.Debugf("Leave request: %+v", &l)
	emptyResponse(w)
	log.Debugf("Leave %s:%s", l.NetworkID, l.EndpointID)
}

func (driver *driver) natOut() error {
	masquerade := []string{
		"POSTROUTING", "-t", "nat",
		"-s", driver.pluginConfig.containerSubnet.String(),
		"-j", "MASQUERADE",
	}
	if _, err := iptables.Raw(
		append([]string{"-C"}, masquerade...)...,
	); err != nil {
		incl := append([]string{"-I"}, masquerade...)
		if output, err := iptables.Raw(incl...); err != nil {
			return err
		} else if len(output) > 0 {
			return &iptables.ChainError{
				Chain:  "POSTROUTING",
				Output: output,
			}
		}
	}
	return nil
}

// return string representation of pluginConfig for debugging
func (d *pluginConfig) String() string {
	str := fmt.Sprintf(" container subnet: [%s],\n", d.containerSubnet.String())
	str = str + fmt.Sprintf("  container gateway: [%s],\n", d.gatewayIP.String())
	str = str + fmt.Sprintf("  host interface: [%s],\n", d.hostIface)
	str = str + fmt.Sprintf("  mmtu: [%d],\n", d.mtu)
	str = str + fmt.Sprintf("  ipvlan mode: [%s]", d.mode)
	return str
}
