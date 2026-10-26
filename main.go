package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
	"periph.io/x/host/v3/rpi"
)

type ScanRequest struct {
	UID       string `json:"uid"`
	Timestamp int64  `json:"timestamp"`
}

type ScanResponse struct {
	Error string `json:"error"`
}

// Change if needed
var (
	resetPin = rpi.P1_22
	irqPin   = rpi.P1_18
)

var (
	noReader bool
)

func init() {
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
	if errors.Is(err, os.ErrNotExist) {
		log.Fatal("config.toml was not found, check and edit config.go if necessary")
	}
	if err != nil {
		log.Fatalf("config err: %v", err)
	}

	uidCh := make(chan []byte)

	if !noReader && reader != nil {
		go func() {
			reader.Start(ctx, uidCh)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case uid, ok := <-uidCh:
			if !ok {
				return
			}

			url := cfg.ServerAddr + "/api/scans/new"

			request := ScanRequest{
				UID:       hex.EncodeToString(uid),
				Timestamp: time.Now().Unix(),
			}

			body, err := json.Marshal(request)
			if err != nil {
				log.Println(err)
			}

			r, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
			if err != nil {
				log.Println(err)
			}

			r.Header.Add("Content-Type", "application/json")

			client := &http.Client{}
			res, err := client.Do(r)
			if err != nil {
				log.Println(err)
			}

			defer res.Body.Close()

			scanRes := &ScanResponse{}
			err = json.NewDecoder(res.Body).Decode(scanRes)
			if err != nil {
				log.Println(err)
			}

			if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
				log.Println(scanRes.Error)
			}
		}

	}
}
