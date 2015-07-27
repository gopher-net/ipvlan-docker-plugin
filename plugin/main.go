package main

import (
	"os"
	"path/filepath"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/gopher-net/ipvlan-docker-plugin/plugin/ipvlan"
)

const (
	version      = "0.1"
	ipvlanL2Mode = "ipvlan-l2.sock"
	ipvlanL3Mode = "ipvlan-l3.sock"
)

var ipvlanMode = ""

func main() {

	var flagSocket = cli.StringFlag{
		Name:  "socket, s",
		Value: "/usr/share/docker/plugins/ipvlan-l2.sock",
		Usage: "listening unix socket",
	}
	var flagDebug = cli.BoolFlag{
		Name:  "debug, d",
		Usage: "enable debugging",
	}
	app := cli.NewApp()
	app.Name = "ipvlan"
	app.Usage = "Docker IPVlan Networking"
	app.Version = "0.0.1"
	app.Flags = []cli.Flag{
		flagDebug,
		flagSocket,
		ipvlan.FlagIpvlanEthIface,
		ipvlan.FlagGateway,
		ipvlan.FlagSubnet,
	}
	app.Before = initEnv
	app.Action = Run
	app.Run(os.Args)
}

func initEnv(ctx *cli.Context) error {
	socketFile := ctx.String("socket")
	// Default loglevel is Info
	if ctx.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.SetOutput(os.Stderr)
	// Verify the path to the plugin socket oath and filename were passed
	socketDir, fileHandle := filepath.Split(socketFile)
	if fileHandle == "" {
		log.Fatalf("Socket file path and name are required. e.g./usr/share/docker/plugins/<plugin_name>.sock")
	}
	// ipvlan has two modes l2 or l3. The plugin defaults to l2
	switch fileHandle {
	case ipvlanL2Mode:
		ipvlanMode = "l2"
		initSock(socketDir, socketFile, fileHandle)
	case ipvlanL3Mode:
		ipvlanMode = "l3"
		initSock(socketDir, socketFile, fileHandle)
	default:
		log.Fatalf("Invalid IPVlan mode [ %s ]. IPVlan has two modes of operation, [ %s ] or [%s ]", fileHandle, ipvlanL2Mode, ipvlanL3Mode)
	}
	return nil
}

// Run initializes the driver
func Run(ctx *cli.Context) {
	var d ipvlan.Driver
	var err error
	if d, err = ipvlan.New(version, ipvlanMode, ctx); err != nil {
		log.Fatalf("unable to create driver: %s", err)
	}
	log.Info("IPVlan network driver initialized successfully")
	if err := d.Listen(ctx.String("socket")); err != nil {
		log.Fatal(err)
	}
}

// removeSock if an old filehandle exists remove it
func removeSock(sockFile string) {
	err := os.Remove(sockFile)
	if err != nil {
		log.Fatalf("unable to remove old socket file [ %s ] due to: %s", sockFile, err)
	}
}

// initSock create the plugin filepath and parent dir if it does not already exist
func initSock(socketDir, socketFile, fileHandle string) {
	if err := os.MkdirAll(socketDir, 0755); err != nil && !os.IsExist(err) {
		log.Warnf("Could not create net plugin path directory: [ %s ]", err)
	}
	// If the plugin socket file already exists, remove it.
	if _, err := os.Stat(socketFile); err == nil {
		log.Debugf("socket file [ %s ] already exists, deleting old file handle..", socketFile)
		removeSock(socketFile)
	}
	log.Debugf("Plugin socket path is [ %s ] with a file handle [ %s ]", socketDir, fileHandle)
}
