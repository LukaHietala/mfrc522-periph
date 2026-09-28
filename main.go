package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
	"periph.io/x/host/v3/rpi"
)

// Before starting here's some things to check:
// - Enable SPI in raspi-config
// - Wire MFRC522 module in and use the pins below to get it working with
// minimal fiddling
//
// Pins
//   3.3V  -> Pin 1  (3.3V Power)
//   GND   -> Pin 6  (Ground)
//   IRQ   -> Pin 18 (GPIO 24).. Many tutorials omit this but periph library
//   uses interrupts instead of polling to be more efficient.
//   MISO  -> Pin 21 (GPIO 9)
//   RST   -> Pin 22 (GPIO 25)
//   SCK   -> Pin 23 (GPIO 11)
//   SDA   -> Pin 24 (GPIO 8)
//   MOSI  -> Pin 19 (GPIO 10)
//
// Once ran the device advertises itself on current network under the service
// name "_rfid_reader._tcp" with label ({label_flag}+{device_mac_address}). The
// main server can scan for these and for a pair. Pair info is stored in "config.json".
//
// Logs can be read from the systemd journal
//
// Tested on:
// - Raspberry Pi 3 Model B+ Rev 1.3
// - Mifare MFRC522 [Bus: SPI0.0, Reset Pin: GPIO25, IRQ Pin: GPIO24]
//
// Other useful stuff:
// - https://github.com/periph/devices/blob/main/mfrc522/example_test.go
// - https://www.nxp.com/docs/en/data-sheet/MFRC522.pdf
// - https://periph.io/device/mf-rc522/
// - https://github.com/hrzlgnm/mdns-browser

var (
	resetPin = rpi.P1_22
	irqPin   = rpi.P1_18
)

var config *Config

var label string
var noReader bool

func init() {
	flag.StringVar(&label, "label", "unnamed", "reader's instance name")
	flag.BoolVar(&noReader, "no-reader", false, "disable physical reader, for debugging")
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var reader *Reader
	if !noReader {
		if _, err := host.Init(); err != nil {
			log.Fatal(err)
		}

		p, err := spireg.Open("")
		if err != nil {
			log.Fatal(err)
		}
		defer p.Close()

		r := NewReader(p, resetPin, irqPin)
		if err := r.Init(); err != nil {
			log.Fatal(err)
		}
		defer r.Close()

		reader = r
		log.Printf("started reader %s", reader.Name())
	} else {
		log.Println("skipping reader init")
	}

	cfg, err := LoadConfig()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	config = cfg

	// TODO: Cleanup
	for {
		if ctx.Err() != nil {
			return
		}

		if config == nil {
			log.Println("no valid config found, starting pairing...")
			cfg, err := StartPairing(ctx, label)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Println("pairing cancelled")
					return
				}
				log.Printf("pairing failed: %v. trying again...", err)
				// Maybe just crash
				time.Sleep(2 * time.Second)
				continue
			}
			config = cfg
			log.Printf("paired successfully with server %s", config.ServerAddr)
		}

		childCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		repairChan := make(chan struct{}, 1)
		uidChan := make(chan []byte, 100)
		tcpClient := NewTCPClient(cfg.ServerAddr)

		var wg sync.WaitGroup

		// TODO: ping server on start

		wg.Go(func() {
			tcpClient.Run(childCtx, uidChan, repairChan)
		})

		if !noReader && reader != nil {
			wg.Go(func() {
				reader.Start(childCtx, uidChan)
			})
		}

		log.Printf("paired with %s", cfg.ServerAddr)

		select {
		case <-repairChan:
			log.Println("server abandoned us, re-pairing...")
			cancel()
			wg.Wait()

			if err := ResetConfig(); err != nil {
				log.Fatalf("failed to reset config: %v.. giving up", err)
			}
			config = nil

		case <-ctx.Done():
			cancel()
			wg.Wait()
			return
		}
	}
}
