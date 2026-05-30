package main

import (
	"flag"
	"fmt"
	"os"

	"displaycameras/internal/displaycameras"
)

const defaultConfigPath = "/etc/displaycameras/config.json"

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to the displaycameras JSON configuration")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [--config path] {run|start|stop|restart|repair|status|positions|rotate|rotaterev|validate}\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	command := flag.Arg(0)
	config, err := displaycameras.LoadConfig(*configPath)
	if err != nil {
		fatal(err)
	}

	controller := displaycameras.NewController(config, displaycameras.ControllerOptions{
		Output: os.Stdout,
	})

	switch command {
	case "validate":
		fmt.Printf("%s is valid\n", *configPath)
	case "run":
		requireRoot()
		fatal(controller.Run())
	case "start":
		requireRoot()
		fatal(controller.Start())
	case "stop":
		requireRoot()
		fatal(controller.Stop())
	case "restart":
		requireRoot()
		fatal(controller.Restart())
	case "repair":
		requireRoot()
		fatal(controller.Repair(false))
	case "status":
		requireRoot()
		fatal(controller.Status())
	case "positions":
		requireRoot()
		fatal(controller.Positions())
	case "rotate":
		requireRoot()
		fatal(controller.Rotate(false))
	case "rotaterev":
		requireRoot()
		fatal(controller.Rotate(true))
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func requireRoot() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "displaycameras must be run as root")
		os.Exit(1)
	}
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}