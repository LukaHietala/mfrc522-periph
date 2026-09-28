package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

func StartPairing(ctx context.Context, label string) (*Config, error) {
	var mac string
	var err error
	for {
		mac, err = getMacAddr()
		if err == nil {
			break
		}

		log.Printf("waiting for valid network interface... %v", err)

		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	name := label + strings.ReplaceAll(mac, ":", "")
	srv, err := zeroconf.Register(name,
		"_rfid_reader._tcp",
		"local.",
		8080,
		[]string{"mac=" + mac},
		nil)
	defer srv.Shutdown()
	if err != nil {
		return nil, fmt.Errorf("mDNS failed %w", err)
	}

	configChan := make(chan Config, 1)

	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("pairing failed (accept): %v", err)
				return
			}

		}
		defer conn.Close()

		var header [2]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			conn.Write([]byte{ackUnpaired})
			log.Printf("pairing failed (read header): %v", err)
			return
		}
		length := binary.BigEndian.Uint16(header[:])

		body := make([]byte, length)
		if _, err := io.ReadFull(conn, body); err != nil {
			if !errors.Is(err, io.EOF) {
				conn.Write([]byte{ackUnpaired})
				log.Printf("pairing failed (read body): %v", err)
				return
			}
		}

		var cfg Config
		if err := json.Unmarshal(body, &cfg); err != nil {
			conn.Write([]byte{ackUnpaired})
			log.Printf("pairing failed (json unmarshal): %v", err)
			return
		}

		conn.Write([]byte{ackOK})

		select {
		case configChan <- cfg:
		default:
		}
	}()

	select {
	case cfg := <-configChan:
		if err := SaveConfig(&cfg); err != nil {
			return nil, fmt.Errorf("failed to save server pairing response to config: %v", err)
		}
		return &cfg, nil
	case <-ctx.Done():
		return nil, errors.New("pairing cancelled")
	}
}

func getMacAddr() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, i := range interfaces {
		if i.Flags&net.FlagUp == 0 {
			continue
		}
		// Not sure if this is necessary
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(i.HardwareAddr) == 0 {
			continue
		}

		// Zeroconf register needs a valid ip and even if there is mac, there might not
		// be a valid ip
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}

		hasIP := false
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			if !ipNet.IP.IsLoopback() && !ipNet.IP.IsUnspecified() {
				hasIP = true
				break
			}
		}

		if !hasIP {
			continue
		}
		return i.HardwareAddr.String(), nil
	}

	return "", errors.New("no valid network interface with an IP address found (loopback skipped)")
}
