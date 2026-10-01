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

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
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
				return nil, ctx.Err()
			}
			log.Printf("pairing accept error: %v", err)
			continue
		}

		cfg, err := handlePairingConn(conn)
		if err != nil {
			log.Printf("pairing attempt failed: %v", err)
			continue
		}

		if err := SaveConfig(cfg); err != nil {
			return nil, fmt.Errorf("failed to save config: %w", err)
		}
		return cfg, nil
	}
}

// Pairing request
// 0-1 (uint16): Length of the config
// 2-.. (JSON): Actual config (server_addr, reader_id, secret_key)
func handlePairingConn(conn net.Conn) (*Config, error) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		_ = sendAck(conn, ackFailed)
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := binary.BigEndian.Uint16(header[:])

	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		_ = sendAck(conn, ackFailed)
		return nil, fmt.Errorf("read body: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		_ = sendAck(conn, ackFailed)
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	_ = sendAck(conn, ackOK)
	return &cfg, nil
}

func sendAck(conn net.Conn, ack byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write([]byte{ack})
	return err
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
