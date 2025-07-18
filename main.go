package main

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
)

var (
	flagConfig string
	flagListen []string
	flagRemote []string
	flagHelp   bool
)

const helpMessage = `traffics:
Usage:
	traffics -l [listen] -r [remote] -c [config] -h
Options:
	-l [listen] : set a listen address and options
	-r [remote] : set the config file path
	-c [config] : set the config file path
	-h/--help : print help message

Example:
	# Start a forward server from local 9500 to 1.2.3.4:48000
	traffics -l "tcp+udp://:9500?remote=example" -r "example://1.2.3.4:48000"
	
	# start from a config file (this will override the command line options like -l and -r)
	traffics -c config.json

See README.md to get full documentation.
`

func main() {
	err := parseFlags()
	if err != nil {
		slog.Error("parse option error", slog.String("error", err.Error()))
		return
	}
	if flagHelp || (len(flagListen) == 0 && len(flagRemote) == 0 && flagConfig == "") {
		fmt.Print(helpMessage)
		return
	}
	var config = NewConfig()
	if flagConfig != "" {
		var (
			bs  []byte
			err error
		)
		if flagConfig == "-" {
			bs, err = io.ReadAll(os.Stdin)
		} else {
			bs, err = os.ReadFile(flagConfig)
		}

		if err != nil {
			slog.Error("read config file failed", slog.String("error", err.Error()))
			return
		}
		err = json.Unmarshal(bs, &config)
		if err != nil {
			slog.Error("parse config file failed", slog.String("error", err.Error()))
			return
		}
	}
	for _, k := range flagListen {
		bind := NewDefaultBind()
		if err := bind.Parse(k); err != nil {
			slog.Error("parse bind failed",
				slog.String("value", k),
				slog.String("error", err.Error()),
			)
			return
		}
		config.Binds = append(config.Binds, bind)
	}
	for _, k := range flagRemote {
		remote := NewDefaultRemote()
		if err := remote.Parse(k); err != nil {
			slog.Error("parse remote failed",
				slog.String("value", k),
				slog.String("error", err.Error()),
			)
			return
		}
		config.Remote = append(config.Remote, remote)
	}

	if len(config.Binds) == 0 || len(config.Remote) == 0 {
		slog.Error("no available bind/remote , quit")
		return
	}

	debug.FreeOSMemory()
	runtime.GC()

	rootCtx, cancel := context.WithCancel(context.Background())
	tf, err := NewTraffics(rootCtx, config)
	if err != nil {
		cancel()
		slog.Error("create new traffics failed", slog.String("error", err.Error()))
		return
	}

	err = tf.Start()
	if err != nil {
		cancel()
		slog.Error("start traffics failed", slog.String("error", err.Error()))
		return
	}
	ch := make(chan os.Signal)
	signal.Notify(ch, unix.SIGINT, os.Interrupt, unix.SIGSTOP, unix.SIGKILL, unix.SIGTERM)

	<-ch
	cancel()
	tf.Close()
}

func parseFlags() error {
	args := os.Args[1:]
	i := 0

	var requireValue = func() string {
		i++
		if i >= len(args) {
			return ""
		}
		return args[i]
	}

	for i < len(args) {
		var key = args[i]
		var value string
		switch key {
		case "-l", "-r", "-c":
			value = requireValue()
			if value == "" {
				return fmt.Errorf("%s option required at least one value after", key)
			}
			break
		case "--help", "-h":
			flagHelp = true
			return nil // returned
		default:
			return fmt.Errorf("unknwon option %s", key)
		}

		switch key {
		case "-r":
			flagRemote = append(flagRemote, value)
		case "-l":
			flagListen = append(flagListen, value)
		case "-c":
			flagConfig = value
		}
		i++
	}
	return nil
}
