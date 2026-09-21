package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
	"periph.io/x/host/v3/rpi"
)

// Before starting here's some things to check:
// - Enable SPI in raspi-config
// - Wire MFRC522 module in and use the pins below to get it working with
// minimal fiddling
// - Add required stuff to .env. Check README and env.go
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
//
// Logs can be read from the systemd journal
//
// Tested on:
// - Raspberry Pi 3 Model B+ Rev 1.3
// - Mifare MFRC522 [Bus: SPI0.0, Reset Pin: GPIO25, IRQ Pin: GPIO24]
//
// Other useful stuff:
// - https://www.nxp.com/docs/en/data-sheet/MFRC522.pdf
// - https://periph.io/device/mf-rc522/
// - https://github.com/periph/devices/blob/main/mfrc522/example_test.go

var (
	resetPin = rpi.P1_22
	irqPin   = rpi.P1_18
)

var config *Config

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := LoadEnv()
	if err != nil {
		log.Fatal(err)
	}
	config = &cfg

	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}

	p, err := spireg.Open("")
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()

	reader := NewReader(p, resetPin, irqPin)
	if err := reader.Init(); err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	log.Printf("started reader %s", reader.Name())

	uidChan := make(chan []byte, 100)
	tcpClient := NewTCPClient(config.ServerAddr)

	go tcpClient.Run(ctx, uidChan)

	reader.Start(ctx, uidChan)
}
