ipvlan-docker-plugin
=================

**Update:** We are working on getting both the MacVlan and IpVlan drivers out of incubation from GopherNet, to upstream Docker Libnetwork natively. Thanks to those who helped with vetting how MacVlan and IpVlan interact with containers, its much appreciated. The notes, PR, early IT tests can be found at: [Macvlan, Ipvlan and 802.1q Trunk Driver Notes](https://gist.github.com/nerdalert/c0363c15d20986633fda). We will leave the drivers up and not deprecate them so that folks can have some example drivers to continue innovating with new network technologies.

Would be interested to hear folks thoughts on getting a pattern togeth for bolting on a external control planes (likely starting with osrg/gobgp because they are awesomez). 

--

ipvlan is a lightweight L2 and L3 network implementation that does not require traditional bridges and is generally pretty kewl.

### Pre-Requisites

#### Kernel Dependencies

The kernel dependency is the ipvlan kernel module support. You can verify if you have it compiled in or not with the following:

```
$ modprobe ipvlan
$ lsmod | grep ipvlan
    ipvlan   24576  0
```
If you get any errors or it doesn't show up from `lsmod` then you probably need to simply upgrade the kernel version. Here is an example upgrade to `v4.3-rc2` that works on both Ubuntu 15.04 and 14.10 along with the similar Debian distributions.

```
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-headers-4.3.0-040300rc2_4.3.0-040300rc2.201509201830_all.deb
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-headers-4.3.0-040300rc2-generic_4.3.0-040300rc2.201509201830_amd64.deb
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-image-4.3.0-040300rc2-generic_4.3.0-040300rc2.201509201830_amd64.deb
$ dpkg -i linux-headers-4.3*.deb linux-image-4.3*.deb
$ reboot
```

As of Docker v1.9 the docker/libnetwork APIs are packaged by default in Docker. Grab the latest or v1.9+ version of Docker from [Latest Linux
binary from Docker](http://docs.docker.com/engine/installation/binaries/). Alternatively `curl -sSL https://get.docker.com/ | sh` or from your
distribution repo or docker repos.

### Ipvlan L2 Mode Instructions

**1.** Start Docker with the following or simply start the service. Version 1.9+ is required.

```
$ docker -v
Docker version 1.9.1, build a34a1d5

# -D is optional debugging
$ docker  daemon -D
```

**2.**  Start the driver.

In the repo directory, use the binary named `ipvlan-docker-plugin-0.3-Linux-x86_64`. The binary can of course be renamed.

```
$ cd binaries
$ ./ipvlan-docker-plugin-0.3-Linux-x86_64 -d

# (optional debug flag) -d
```

**3.** Create a network with Docker

Note, the subnet needs to correspond to the master interface.

The mode and host interface options are required. Use the interface on the subnet you want the containers to be on. In the following example, `eth1` of the host OS is on `192.168.1.0/24`.

- `-o host_iface=eth1`
- `-o mode=l2`

```
$ docker network  create  -d ipvlan  --subnet=192.168.1.0/24 --gateway=192.168.1.1 -o host_iface=eth1 -o mode=l2  net1
```

**4.** Start some docker containers

Run some containers, specify the network and verify they can ping one another

```
$ docker run --net=net1 -it --rm ubuntu
```

### Ipvlan 802.1q Trunk L2 Mode Example Usage ###

This example can also be run up to a ToR switch with a .1q trunk. To test on a localhost, Vlan 20 should not be able to ping Vlan 30 without being routed by an upstream router/gateway. All containers inside of the Vlan should be able to ping one another. The default namespace (example: eth0 on docker host) is not reachable via icmp per ipvlan arch.

Start the plugin the same as the above example (-d for debug) along with the Docker daemon running `$ docker daemon`:

```
$ ./ipvlan-docker-plugin-0.3-Linux-x86_64 -d
```

**Vlan ID 20**

```
# create a new subinterface tied to dot1q vlan 20
ip link add link eth1 name eth1.20 type vlan id 20

# enable the new sub-interface
$ ip link set eth1.20 up

# now add networks and hosts as you would normally by attaching to the master (sub)interface that is tagged
$ docker network  create  -d ipvlan  --subnet=192.168.20.0/24 --gateway=192.168.20.1 -o host_iface=eth1.20 ipvlan20
$ docker run --net=ipvlan20 -it --name ivlan_test1 --rm ubuntu
$ docker run --net=ipvlan20 -it --name ivlan_test2 --rm ubuntu

# ivlan_test1 should be able to ping ivlan_test2 now.
```

**Vlan ID 30**

```
# create a new subinterface tied to dot1q vlan 30
$ ip link add link eth1 name eth1.30 type vlan id 30

# enable the new sub-interface
$ ip link set eth1.30 up

# now add networks and hosts as you would normally by attaching to the master (sub)interface that is tagged
$ docker network  create  -d ipvlan  --subnet=192.168.30.0/24 --gateway=192.168.30.1 -o host_iface=eth1.30 ipvlan130
$ docker run --net=ipvlan30 -it --name ivlan_test3 --rm ubuntu
$ docker run --net=ipvlan30 -it --name ivlan_test4 --rm ubuntu

# ivlan_test3 should be able to ping ivlan_test4 now.
```

Docker networks are now persistant after a reboot. The plugin does not currently support dealing with unknown networks. That is a priority next. To remove all of the network configs on a docker daemon restart you can simply delete the directory with: `rm  /var/lib/docker/network/files/*`

### Example L3 Mode

Ipvlan L3 mode requires a route to be added in the default namespace as well as be advertised or summarized to the rest of the network. This makes it both highly scalable and very attractive to integrate into either the underlay IGP/EGPs or exchange prefixes into overlays with distributed datastores or gateway protos. You can simply replace `L2` with `L3` to do so but since the routes need to be orchestrated throughout a cluster take a look at the next section for the [Go-BGP L3 mode integration](https://github.com/gopher-net/ipvlan-docker-plugin#go-bgp-l3-mode-integration).

### Go-BGP L3 mode integration

See the [README](https://github.com/gopher-net/ipvlan-docker-plugin/blob/master/plugin/routing/routing-manager.md) in the Go-BGP integration section (killer next-gen BGP daemon from our friends at [github.com/osrg/gobgp](https://github.com/osrg/gobgp)).

### Notes and General IPVlan Caveats


- There can only be one network type bound to the host interface at any given time. Example: Macvlan Bridge or IPVlan L2. There is no mixing.
- The specified gateway is external to the host or at least not defined by the driver itself.
- Multiple drivers can be active at any time. However, Macvlan and Ipvlan are not compatable on the same master interface (e.g. eth0).
- You can create multiple networks and have active containers in each network as long as they are all of the same mode type.
- Each network is isolated from one another. Any container inside the network/subnet can talk to one another without a reachable gateway.
- Containers on separate networks cannot reach one another without an external process routing between the two networks/subnets.


### Dev and issues

Use [Godep](https://github.com/tools/godep) for dependencies.

Install and use Godep with the following:

```
$ go get github.com/tools/godep
# From inside the plugin directory where the Godep directory is restore the snapshotted dependencies used by libnetwork:
$ godep restore
```

 There is a `godbus/dbus` version that conflicts with `vishvananda/netlink` that will lead to this error at build time. This can appear as libnetwork issues when in fact it is 3rd party drivers. Libnetwork also uses Godep for versioning so using those versions would be just as good or even better if keeping with the latest experimental nightly Docker builds:

Example of the godbus error:

```
../../../docker/libnetwork/iptables/firewalld.go:75: cannot use c.sysconn.Object(dbusInterface, dbus.ObjectPath(dbusPath)) (type dbus.BusObject) as type *dbus.
Object in assignment: need type assertion
```

- If you dont want to use godep @Orivej graciously pointed out the godbus dependency in issue #5:

"You need a stable godbus that you can probably get with:"
```
cd $GOPATH/src/github.com/godbus/dbus
git checkout v2
```

 - Another option would be to use godep and sync your library with libnetworks.

```
go get github.com/tools/godep
git clone https://github.com/docker/libnetwork.git
cd libnetwork
godep restore
```
