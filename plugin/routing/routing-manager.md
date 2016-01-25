L3 Routing Mode
====================
In ipvlan L3 mode the slave devices will not receive nor can send multicast / broadcast traffic.
In this mode TX processing upto L3 happens on the stack instance attached to the slave device and packets are switched to the stack instance of the master device for the L2 processing and routing from that instance will be used before packets are queued on the outbound device.
So in ipvlan plugin l3routing mode, plugin advertise paths for containers by routing manager.

##GoBGP Routing Manager

GoBGP is an open source BGP(Boder Gateway Protocol) implementation,[GoBGP](http://golang.org/). 
In this routing manager advertise paths by BGP protocol.

###Pre-Requistes

####Install GoBGP
```bash
$ go get github.com/osrg/gobgp/gobgpd
$ go get github.com/osrg/gobgp/gobgp
```
###Starting GoBGP

First, you should prepare GoBGP configuration file.
In this situation,

```bash
                                                                  
                                                                 
     +-------------------------+      +-------------------------+    
     |   Host: host1           |      |  Host: host2            |    
     |                         |      |                         |    
     |                         |      |                         |    
     |  eth0:192.168.250.1/24  |      |  eth0:192.168.251.1/24  |    
     +------------#------------+      +-------------#-----------+    
                  #                                 #               
                  ###################################               
                                # Bridge or Router                

```
At host1, create configuration file 'gobgp.yml' like below.

```toml
[global.config]
  as = 64512
  router-id = "192.168.255.1"

[[neighbors]]
  [neighbors.config]
    neighbor-address = "10.0.255.1"
    peer-as = 65001
```
Please confirm L3 reachability between host1 and host2.
And start GoBGP.

```bash
$ sudo -E gobgpd -t yaml gobgpd.yml
{"level":"info","msg":"Peer 10.0.251.1 is added","time":"2015-04-06T20:32:28+09:00"}
```
###Stating Ipvlan L3-routing Mode

OK, Let's start ipvlan plugin and create docker container!

At host1,

```bash
go run main.go --host-interface=eth0 -d --mode=l3routing --routemng=gobgp
docker network create -d ipvlan --subnet=10.10.1.0/24 ipvlanl3route
docker run --net=ipvlanl3route -it --rm busybox
```
Finished. 
ipvlan plugin automatically write route to routing table and add path to GpBGP.
GoBGP advertise path to neighbors.
At host2 also create network and docker run.

**Note:** Create network with unique subnet among all hosts.

docker containers can ping each other in multi host.

###Auto Configuration

If you configure Docker Engine daemon to use key-value store with two parameters ```--cluster-store``` and ```--cluster-advertise```, you can run ipvlan l3-routing mode without config files.
Start GoBGP only this command.

```bash
$ sudo -E gobgpd
```
And start Ipvlan plugin

```bash
go run main.go --host-interface=eth0 -d --mode=l3routing --routemng=gobgp
docker network create -d ipvlan --subnet=10.10.1.0/24 ipvlanl3routehost1
```

Add global config and neighbor information (other hosts in cluster) to GoBGP.

**Note:** Create network with unique **name and subnet** among all hosts.
