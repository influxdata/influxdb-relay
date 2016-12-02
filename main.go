package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	log "github.com/Sirupsen/logrus"
	"github.com/influxdata/influxdb-relay/relay"
)

const (
	LOG = "relay.log"
)

var (
	configFile = flag.String("config", "", "Configuration file to use")
)

func main() {
	flag.Parse()

	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "Missing configuration file")
		flag.PrintDefaults()
		os.Exit(1)
	}

	cfg, err := relay.LoadConfigFile(*configFile)
	if err != nil {
		log.Fatal("Problem loading config file", err)
	}

	err = relay.Mkdir(cfg.Log.Path)
	if err != nil {
		log.Fatal("mkdir error:", err)
	}

	logFile := cfg.Log.Path + "/" + LOG
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		log.Fatal("error opening file: %v", err)
	}
	// don't forget to close it
	defer f.Close()

	logInit(f, cfg.Log.Level)

	r, err := relay.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		<-sigChan
		r.Stop()
	}()

	log.Info("starting relays...")
	r.Run()
}

func logInit(f *os.File, level string) {
	//log.SetFormatter(&log.JSONFormatter{})
	log.SetFormatter(&log.TextFormatter{})
	log.SetOutput(f)

	l, err := log.ParseLevel(level)

	if err != nil {
		log.Fatalln("Unknown log level")
	} else {
		log.SetLevel(l)
	}
}
