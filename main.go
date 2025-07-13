package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"log/slog"
	"os"
	"os/signal"
)

var (
	flagConfig string
	flagListen string
	flagRemote string
	flagHelp   bool
)

func init() {
	flag.StringVar(&flagListen, "l", "", "set the listen address and options")
	flag.StringVar(&flagConfig, "c", "", "set the config file path")
	flag.StringVar(&flagRemote, "r", "", "set the forward remote address and options")
	flag.BoolVar(&flagHelp, "help", false, "print help message")
}

const helpMessage = `traffics:
Usage:
	traffics -l [listen] -r [remote] -c [config] --help
Options:
	-l [listen] : set a listen address and options
	-r [remote] : set the config file path
	-c [config] : set the config file path

Example:
	# Start a forward server from local 9500 to 1.2.3.4:48000
	traffics -l "tcp+udp://:9500?remote=example" -r "1.2.3.4:48000?name=example"
	
	# start from a config file (this will override the command line options like -l and -r)
	traffics -c config.json
`

func main() {
	flag.Parse()
	if flagHelp || (flagListen == "" && flagRemote == "" && flagConfig == "") {
		fmt.Print(helpMessage)
		os.Exit(0)
	}
	var config = NewConfig()
	if flagConfig == "" && (flagListen != "" || flagRemote != "") {
		listen := NewDefaultBind()
		err := listen.Parse(flagListen)
		if err != nil {
			slog.Error("parse failed", slog.String("listen", flagListen), slog.String("error", err.Error()))
			os.Exit(1)
		}

		remote := NewDefaultRemote()
		err = remote.Parse(flagRemote)
		if err != nil {
			slog.Error("parse failed", slog.String("remote", flagRemote), slog.String("error", err.Error()))
			os.Exit(1)
		}
		config.Binds = append(config.Binds, listen)
		config.Remote = append(config.Remote, remote)
	}

	if flagConfig != "" {
		bs, err := os.ReadFile(flagConfig)
		if err != nil {
			slog.Error("read config file failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		err = json.Unmarshal(bs, &config)
		if err != nil {
			slog.Error("parse config file failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	tf, err := NewTraffics(rootCtx, config)
	if err != nil {
		cancel()
		slog.Error("create new traffics failed", slog.String("error", err.Error()))
		os.Exit(1)
		return
	}
	err = tf.Start()
	if err != nil {
		cancel()
		slog.Error("start traffics failed", slog.String("error", err.Error()))
		os.Exit(1)
		return
	}
	ch := make(chan os.Signal)
	signal.Notify(ch, unix.SIGINT, os.Interrupt, unix.SIGSTOP, unix.SIGKILL, unix.SIGTERM)

	<-ch
	cancel()
	tf.Close()
}
