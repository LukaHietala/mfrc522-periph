package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

func StartPairing(ctx context.Context, label string, port int) (*Config, error) {
	mac, err := getMacAddr(ctx)
	if err != nil {
		return nil, err
	}

	// Name must be unique, since we pair based on it
	name := label + "-" + strings.ReplaceAll(mac, ":", "")
	srv, err := zeroconf.Register(name,
		"_rfid_reader._tcp",
		"local.",
		port,
		[]string{"mac=" + mac},
		nil)
	if err != nil {
		return nil, fmt.Errorf("mDNS failed %w", err)
	}
	defer srv.Shutdown()
	log.Printf("registered as %s (%s)", name, mac)

	configChan := make(chan Config, 1)

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var header [2]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			conn.Write([]byte{ackFailed})
			log.Printf("pairing failed (read header): %v", err)
			return
		}
		length := binary.BigEndian.Uint16(header[:])

		body := make([]byte, length)
		if _, err := io.ReadFull(conn, body); err != nil {
			conn.Write([]byte{ackFailed})
			log.Printf("pairing failed (read body): %v", err)
			return
		}

		var cfg Config
		if err := json.Unmarshal(body, &cfg); err != nil {
			conn.Write([]byte{ackFailed})
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
			return nil, fmt.Errorf("failed to save config: %w", err)
		}
		return &cfg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func getMacAddr(ctx context.Context) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		interfaces, err := net.Interfaces()
		if err == nil {
			for _, i := range interfaces {
				if i.Flags&net.FlagUp == 0 {
					continue
				}
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

				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok {
						if !ipNet.IP.IsLoopback() && !ipNet.IP.IsUnspecified() {
							return i.HardwareAddr.String(), nil
						}
					}
				}
			}
		}

		select {
		case <-ticker.C:
			log.Println("could not find device mac address, trying again...")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
