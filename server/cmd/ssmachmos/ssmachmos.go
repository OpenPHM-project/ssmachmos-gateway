// Package main is where the ssmachmos program starts.
// "ssmachmos serve" runs the gateway; any other command runs the CLI, which talks to the running gateway.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jukuly/ss_machmos/server/internal/api"
	"github.com/jukuly/ss_machmos/server/internal/cli"
	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
	"github.com/jukuly/ss_machmos/server/internal/server"
)

// serve starts the gateway: loads saved data, turns on Bluetooth, then waits for CLI/GUI commands.
func serve() {
	var err error
	// if the server is already running, quit
	// Do not kill other instances, they might be doing something important.
	conn, err := cli.OpenConnection()
	if err == nil {
		out.Logger.Println("Server already runnning, Quit.")
		conn.Close()
		return
	}

	out.Logger.Println("Loading local config...")

	var gateway *model.Gateway = &model.Gateway{}
	err = model.LoadSensors()
	// BUG: on a fresh install sensors.json doesn't exist yet, so the gateway refuses to start.
	if err != nil {
		out.Logger.Println(err.Error())
		return
	}
	// Errors ignored on purpose: a missing history file is normal on first run.
	model.LoadSensorHistory()
	err = model.LoadSettings(gateway, model.GATEWAY_FILE)
	// Keep going without settings, so the user can still set them with "ssmachmos config".
	if err != nil {
		out.Logger.Println("Error loading Gateway settings. Run 'ssmachmos config --id <gateway-id>' and 'ssmachmos config --password <gateway-password>' to set the Gateway settings.")
	}

	out.Logger.Println("Starting bluetooth advertisement...")
	err = server.Init(gateway)
	if err != nil {
		out.Logger.Println("Error:", err)
	} else {
		err = server.StartAdvertising()
		if err != nil {
			out.Logger.Println("Error:", err)
		}
	}

	if err == nil {
		out.Logger.Println("Done initializing server.")
	} else {
		out.Logger.Println("Error while initializing server. Is bluetooth enabled?")
		return
	}
	// BUG: api.Start's error is ignored, so if the socket can't open, the gateway quits with no message.
	api.Start()
}

// main reads the command typed after "ssmachmos" and runs it.
// Words starting with "--" are options; the rest are arguments.
func main() {
	// the user must provide at least one argument (the command)
	as := os.Args[1:]
	if len(as) == 0 {
		fmt.Println("Usage: ssmachmos <command> [options] [arguments]")
		return
	}

	options := make([]string, len(as)-1)
	args := make([]string, len(as)-1)

	var (
		i int
		j int
	)
	// skip the first argument (the command)
	for _, a := range as[1:] {
		if strings.HasPrefix(a, "--") {
			options[i] = a
			i++
		} else {
			args[j] = a
			j++
		}
	}

	options = options[:i]
	args = args[:j]

	if as[0] == "serve" {
		out.InitSyslog()
		// "--no-console" restarts the program in the background, detached from the terminal.
		// BUG: only checks the first option, so "--no-console" after another option is ignored.
		if len(options) > 0 && options[0] == "--no-console" {
			process, err := os.StartProcess(os.Args[0], []string{os.Args[0], "serve"}, &os.ProcAttr{
				Files: []*os.File{nil, nil, nil},
				Env:   os.Environ(),
			})
			if err != nil {
				out.Logger.Println("Error:", err)
				return
			}
			err = process.Release()
			if err != nil {
				out.Logger.Println("Error:", err)
			}
			out.Logger.Println("ssmachmos started.")
			// fmt.Println("Server started.")
			// fmt.Println("To stop the server, run 'ssmachmos stop'.")
			// fmt.Println("To view the live server logs, run 'ssmachmos logs'.")
		} else {
			// Run in TTY
			serve()
		}
		return
	}

	if as[0] == "help" {
		cli.Help(args)
		return
	}

	// open a unix domain socket connection to the server
	conn, err := cli.OpenConnection()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	// Print messages from the gateway in the background while the command runs.
	go cli.Listen(conn)
	defer conn.Close()

	switch as[0] {
	case "logs":
		cli.Logs(conn)
	case "list":
		cli.List(conn)
	case "view":
		cli.View(options, args, conn)
	case "pair":
		cli.Pair(args, conn)
	case "forget":
		cli.Forget(args, conn)
	case "config":
		cli.Config(options, args, conn)
	case "stop":
		cli.Stop(conn)
	case "ctl":
		cli.Read(conn)
	default:
		fmt.Printf("Unknown command: %s\n", as[0])
	}
}