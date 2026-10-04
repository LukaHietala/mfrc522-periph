package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
	"periph.io/x/host/v3/rpi"
)

// Change if needed
var (
	resetPin = rpi.P1_22
	irqPin   = rpi.P1_18
)

var config *Config

var (
	label    string
	noReader bool
)

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

	// Disgusting context soup
	for {
		if ctx.Err() != nil {
			return
		}

		if config == nil {
			log.Println("no valid config found, starting pairing...")
			cfg, err := StartPairing(ctx, label, DefaultPairingPort)
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

		childCtx, childCancel := context.WithCancel(ctx)

		repairChan := make(chan struct{}, 1)
		uidChan := make(chan []byte)
		tcpClient := NewTCPClient(config.ServerAddr)

		var wg sync.WaitGroup

		if err := pingServer(config.ServerAddr, 2*time.Second); err != nil {
			log.Printf("warning, could not ping server %s: %v", config.ServerAddr, err)
		} else {
			log.Printf("successfully reached %s", config.ServerAddr)
		}

		// Sends uids
		wg.Go(func() {
			tcpClient.Run(childCtx, uidChan, repairChan)
		})

		// Listens for server pings
		pingAddr := fmt.Sprintf(":%d", DefaultPingListenerPort)
		wg.Go(func() {
			if err := startPingListener(childCtx, pingAddr); err != nil && !errors.Is(err, net.ErrClosed) {
				log.Printf("ping listener error: %v", err)
			}
		})

		// Actual reader
		if !noReader && reader != nil {
			wg.Go(func() {
				reader.Start(childCtx, uidChan)
			})
		}

		select {
		case <-repairChan:
			log.Println("server abandoned us, re-pairing...")
			childCancel()
			wg.Wait()

			if err := ResetConfig(); err != nil {
				log.Fatalf("failed to reset config: %v.. giving up", err)
			}
			config = nil

		case <-ctx.Done():
			childCancel()
			wg.Wait()
			return
		}
	}
}

// TODO: Make actual ping
func pingServer(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func startPingListener(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start ping listener: %w", err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("ping accept error: %v", err)
			continue
		}

		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var ack [1]byte
		if _, err := io.ReadFull(conn, ack[:]); err != nil {
			conn.Close()
			log.Printf("ping read error: %v", err)
			continue
		}

		if ack[0] == ackPing {
			_, err := conn.Write([]byte{ackPong})
			if err != nil {
				log.Printf("ping response write error: %v", err)
			}
		}
	}
}
