ipvlan-docker-plugin
=================

ipvlan is a lightweight L2 and L3 network implementation that does not require traditional bridges. The purpose of this is to have references for plugging into the docker networking APIs which are now available as part of libnetwork. Libnetwork is still under development and is considered as experimental at this point.


### Pre-Requisites

1. Install the Docker experimental binary from the instructions at: [Docker Experimental](https://github.com/docker/docker/tree/master/experimental). (stop other docker instances)
	- Quick Experimental Install: `wget -qO- https://experimental.docker.com/ | sh`

2. The kernel version for ipvlan needs to be 4.0+. I have tested this with v4.05. and up. Here is an example tested version and kernel upgrade procedure:

```
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-headers-4.3.0-040300rc2_4.3.0-040300rc2.201509201830_all.deb
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-headers-4.3.0-040300rc2-generic_4.3.0-040300rc2.201509201830_i386.deb
$ wget http://kernel.ubuntu.com/~kernel-ppa/mainline/v4.3-rc2-unstable/linux-image-4.3.0-040300rc2-generic_4.3.0-040300rc2.201509201830_i386.deb
sudo dpkg -i linux-headers-4.3*.deb linux-image-4.2*.deb
sudo reboot
```

### QuickStart Instructions (L2 Mode)


1. Start Docker with the following. **TODO:** How to specify the plugin socket without having to pass a bridge name `foo` since ipvlan/macvlan do not use traditional bridges. This example is running docker in the foreground so you can see the logs realtime.

```
    docker -d --default-network=ipvlan:foo`
```

2. Download the plugin binary. A pre-compiled x86_64 binary can be downloaded from the [binaries](https://github.com/gopher-net/ipvlan-docker-plugin/binaries) directory.

```
	$ wget -O ./ipvlan-docker-plugin https://github.com/gopher-net/ipvlan-docker-plugin/binaries/ipvlan-docker-plugin-0.1-Linux-x86_64
	$ chmod +x ipvlan-docker-plugin
```

3. In a new window, start the plugin with the following. Replace the values with the appropriate subnet and gateway to match the network the docker host is attached to. In the following the nic `eth1` is attached to a network segment with other hosts on the `192.168.1.0/24` subnet along with the gateway `192.168.1.1`.

Here is the `eth1` ip configuration to make help ensure the role of the parent ipvlan interface is clear:

```
    $ ip add show eth1
    3: eth1: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UP group default qlen 1000
        link/ether 00:50:56:27:87:2f brd ff:ff:ff:ff:ff:ff
        inet 192.168.1.254/24 brd 192.168.1.255 scope global eth1
```
    
Start the driver in L2 mode:

```
$ ./ipvlan-docker-plugin \
    --gateway=192.168.1.1 \
    --ipvlan-subnet=192.168.1.0/24 \
    --host-interface=eth1 \
    --mode=l2

# Or in one line:

$ ./ipvlan-docker-plugin --host-interface=eth1 -d --mode=l2 --gateway=192.168.1.1  --ipvlan-subnet=192.168.1.0/24
```

- For debugging, or just extra logs from the sausage factory, add the debug flag `./ipvlan-docker-plugin -d`.

4. Run some containers and verify they can ping one another with `docker run -it --rm busybox` or `docker run -it --rm ubuntu` etc, any other docker images you prefer. Alternatively,`docker run -itd busybox`

    * Or use the script `release-the-whales.sh` in the `scripts/` directory to launch a bunch of lightweight busybox instances to see how well the plugin scales for you. It can also serve as temporary integration testing. See the comments in the script for usage. There are some default values in the driver used if no values are specified with the binary or flagged as mandatory. The driver's CLI will tell you what is required and any default values/constructors being used.

### Example L3 Mode 

**Note:** L3 mode needs to use a **different** subnet then the parent interface. It also requires a static route in the global namespace that is added by the driver but it also requires other hosts to know about the subnet you have tucked away in the container namespace. Out of the box L3 mode will not be able to ping between different hosts without the routes being distributed across all of the hosts. You can manually add it into the global namespace with something like `ip route add 10.1.1.0/24 <x.x.x.x Next-hop IP could be another endpoint or gateway>`. 

If a nerd for networking, take a look at a /32 multi-host driver that is this plugin (IPVlan) plus a BGP daemon to distribute the container host routes (or /24 for example for an entire docker host instead of /32s). The routes get distributed to all other hosts in the cluster using the same distributed system protocol that scales the Internet. The BGP plugin proof of concept is at [nerdalert/bgp-ipvlan-docker](https://github.com/nerdalert/bgp-ipvlan-docker).

```
$ ./ipvlan-docker-plugin \
        --host-interface eth1 \
        --ipvlan-subnet=10.1.1.0/24 \
        --mode=l3

# Or with Go using:
go run main.go --host-interface eth1 -d --mode l3 --ipvlan-subnet=10.1.1.0/24
```

Lastly start up some containers and check reachability:

```
docker run -i -t --rm ubuntu
```

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





