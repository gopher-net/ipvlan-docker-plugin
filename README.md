ipvlan-docker-plugin
=================

TODO: Readme

Example L2 Mode:

```
go run main.go --host-interface eth1 -d --mode l2

or:

go run main.go --host-interface eth1 -d --mode l2 --gateway=10.1.1.1 --ipvlan-subnet=10.1.1.0/24
```

Example L3 Mode:

```
go run main.go --host-interface eth1 -d --mode l3 --ipvlan-subnet=10.1.1.0/24
```

Run Docker with:

- Ignore `foo`. It is a default bridge but irrelavant since ipvlan doesn't use a traditional bridge but lighter weight pseudo bridges in the case of l2 mode. l3 mode requires a static route in the default ns that points to the container netns which isnt implemnted here yet.
```
docker -d -D --default-network=ipvlan:foo
```

