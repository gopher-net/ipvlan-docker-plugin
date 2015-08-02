ipvlan-docker-plugin
=================

ipvlan is a lightweight L2 and L3 network implementation that does not require traditional bridges. The purpose of this is to have references for plugging into the docker networking APIs which are now available as part of libnetwork. Libnetwork is still under development and is considered as experimental at this point.


### Pre-Requisites

1. Install the Docker experimental binary from the instructions at: [Docker Experimental](https://github.com/docker/docker/tree/master/experimental). (stop other docker instances)
	- Quick Experimental Install: `wget -qO- https://experimental.docker.com/ | sh`

### QuickStart Instructions


1. Start Docker with the following. **TODO:** How to specify the plugin socket without having to pass a bridge name since ipvlan/ipvlan dont use traditional bridges. This example is running docker in the foreground so you can see the logs realtime.

    ```
    docker -d --default-network=ipvlan:foo`
    ```

2. Download the plugin binary. A pre-compiled x86_64 binary can be downloaded from the [binaries](https://github.com/gopher-net/ipvlan-docker-plugin/binaries) directory.

	```
	$ wget -O ./ipvlan-docker-plugin https://github.com/gopher-net/ipvlan-docker-plugin/binaries/ipvlan-docker-plugin-0.1-Linux-x86_64
	$ chmod +x ipvlan-docker-plugin
	```

3. In a new window, start the plugin with the following. Replace the values with the appropriate subnet and gateway to match the network the docker host is attached to.

    ```
    	$ ./ipvlan-docker-plugin \
    	        --gateway=192.168.1.1 \
    	        --ipvlan-subnet=192.168.1.0/24 \
    	        --host-interface=eth1 \
    	         -mode=bridge
    # Or in one line:

	$ ./ipvlan-docker-plugin --host-interface eth1 -d --mode=l2 --gateway=192.168.1.1  --ipvlan-subnet=192.168.1.0/24
    ```

    for debugging, or just extra logs from the sausage factory, add the debug flag `./ipvlan-docker-plugin -d`.

4. Run some containers and verify they can ping one another with `docker run -it --rm busybox` or `docker run -it --rm ubuntu` etc, any other docker images you prefer. Alternatively,`docker run -itd busybox`

    * Or use the script `release-the-whales.sh` in the `scripts/` directory to launch a bunch of lightweight busybox instances to see how well the plugin scales for you. It can also serve as temporary integration testing until we get CI setup. See the comments in the script for usage. Keep in mind, the subnet defined in `cli.go` is the temporarily hardcoded network address `192.168.1.0/24` and will hand out addresses starting at `192.168.1.2`. This is very temporary until we bind CLI options to the driver data struct.


Example L2 Mode:

```
$ ./ipvlan-docker-plugin \
        --host-interface eth1 \
        --gateway=192.168.1.1 \
        --ipvlan-subnet=192.168.1.0/24 \
        --mode=l2

# Or with Go using:
go run main.go -d --host-interface eth1 -d --mode l2 --gateway=192.168.1.1  --ipvlan-subnet=192.168.1.0/24
```

Example L3 Mode (**Note:** L3 mode needs to use a **different** subnet then the parent interface. It also requires a static route in the global namespace that is not yet implemented):

```
$ ./ipvlan-docker-plugin \
        --host-interface eth1 \
        --ipvlan-subnet=10.1.1.0/24 \
        --mode=l3

# Or with Go using:
go run main.go --host-interface eth1 -d --mode l3 --ipvlan-subnet=10.1.1.0/24
```

Run Docker with:

- Ignore `foo`. It is a default bridge but irrelavant since ipvlan doesn't use a traditional bridge but lighter weight pseudo bridges in the case of l2 mode. l3 mode requires a static route in the default ns that points to the container netns which isnt implemnted here yet.
```
docker -d -D --default-network=ipvlan:foo
```
