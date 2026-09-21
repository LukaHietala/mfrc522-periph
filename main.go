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

// Before starting make sure to have the mfrc522 card connected and SPI (in
// raspi config) enabled.
// Remember to also update the pins and server address below.
//
// Example layout:
// SDA to pin 24 (gpio 8)
// SCK to pin 23 (gpio 11)
// MOSI to pin 19 (gpio 10)
// MISO to pin 21 (gpio 9)
// GND to pin 6 (ground)
// RST to Pin 22 (gpio 25, below!)
// 3.3v to pin 1 (3v3 power)
// IRQ to pin 18 (gpio 24, below!)
//
// Logs can be read from systemd journal
//
// Verify that these match with the setup, change if needed (if you followed the
// example layout you shouln't have to touch these :D)
//
// Tested on:
// Raspberry Pi 3 Model B Plus Rev 1.3
// Mifare MFRC522 [bus: SPI0.0, reset pin: GPIO25, irq pin: GPIO24]

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
