package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/devices/v3/mfrc522"
)

type Reader struct {
	port     spi.PortCloser
	resetPin gpio.PinIO
	irqPin   gpio.PinIO
	dev      *mfrc522.Dev

	readInterval time.Duration
	debounceTime time.Duration // per card
}

func NewReader(port spi.PortCloser, resetPin, irqPin gpio.PinIO) *Reader {
	return &Reader{
		port:         port,
		resetPin:     resetPin,
		irqPin:       irqPin,
		readInterval: 1 * time.Second,
		debounceTime: 1 * time.Second,
	}
}

func (r *Reader) Init() error {
	dev, err := mfrc522.NewSPI(r.port, r.resetPin, r.irqPin)
	if err != nil {
		return fmt.Errorf("failed to init mfrc522: %w", err)
	}

	if err := dev.SetAntennaGain(5); err != nil {
		dev.Halt()
		return fmt.Errorf("failed to set antenna gain: %w", err)
	}

	r.dev = dev
	return nil
}

func (r *Reader) Close() error {
	if r.dev != nil {
		return r.dev.Halt()
	}
	return nil
}

func (r *Reader) Name() string {
	return r.dev.String()
}

func (r *Reader) Start(ctx context.Context, uidChan chan<- []byte) {
	if r.dev == nil {
		log.Println("call init first :)")
		return
	}

	var lastUID string
	var lastRead time.Time

	for {
		if ctx.Err() != nil {
			return
		}

		uid, err := r.dev.ReadUID(r.readInterval)
		// Some devices tend to send wrong data while RFID chip is already detected
		// but still "too far" from a receiver
		if err != nil {
			continue
		}

		currentUID := hex.EncodeToString(uid)

		if currentUID == lastUID && time.Since(lastRead) < r.debounceTime {
			continue
		}

		lastUID = currentUID
		lastRead = time.Now()

		log.Printf("read uid: %s len: %d", currentUID, len(uid))

		select {
		case <-ctx.Done():
			return
		case uidChan <- uid:
		}
	}
}
