package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync/atomic"
	"time"
)

// Protocol (big endian)
// Byte 0 (uint8): Magic byte. Server does a quick check with this to validate
// that the packet is valid.
//
// Byte 1 (uint8): UID length (4 or 7). Length of the rfid card uid.
//
// Byte 2-9 (int64/uint64): Unix timestamp. Used for logging and to prevent
// replay attacks. Server checks the clock skew and makes sure it's in valid
// range.
//
// Byte 10-13 (uint32): Seq. Sequence number. Server drops all packets with seq
// <= last seq.
//
// Byte 14-45 ([32]uint8): HMAC-SHA256 hash. Sum of headers and uid. Works as
// the packet nonce. Server makes sure that the sender is allowed to actually
// send packets with this and makes sure that fields like timestamp and seq
// can't be forged.
//
// Byte 46..: UID data. The rfid uid.

const (
	// RFID uid should always be either 4 or 7 bytes
	// https://github.com/periph/devices/blob/main/mfrc522/mfrc522.go#L142
	minPayloadLen = 4
	maxPayloadLen = 7

	magicByte = 0xAA
)

var lastSeq atomic.Uint32

type UIDPacket struct {
	Magic     uint8
	Length    uint8
	Timestamp int64
	Seq       uint32
	Hash      [32]byte
	UID       []byte
}

type TCPClient struct {
	addr        string
	dialTimeout time.Duration
}

func NewTCPClient(addr string) *TCPClient {
	return &TCPClient{
		addr:        addr,
		dialTimeout: 2 * time.Second,
	}
}

// Marshals uid packet to binary with right headers
func (p *UIDPacket) MarshalBinary() ([]byte, error) {
	uidLen := len(p.UID)
	if uidLen != minPayloadLen && uidLen != maxPayloadLen {
		return nil, fmt.Errorf("invalid uid length %d: expected 4 or 7 bytes", uidLen)
	}

	p.Magic = magicByte
	p.Length = uint8(uidLen)
	p.Timestamp = time.Now().Unix()

	p.Seq = lastSeq.Add(1) - 1

	mac := hmac.New(sha256.New, []byte(config.SecretKey))

	_ = binary.Write(mac, binary.BigEndian, p.Magic)
	_ = binary.Write(mac, binary.BigEndian, p.Length)
	_ = binary.Write(mac, binary.BigEndian, p.Timestamp)
	_ = binary.Write(mac, binary.BigEndian, p.Seq)
	mac.Write(p.UID)

	copy(p.Hash[:], mac.Sum(nil))

	// magic + length + timestamp + seq + hash + uid size
	buf := make([]byte, 1+1+8+4+32+uidLen)

	buf[0] = p.Magic
	buf[1] = p.Length

	binary.BigEndian.PutUint64(buf[2:10], uint64(p.Timestamp))
	binary.BigEndian.PutUint32(buf[10:14], p.Seq)

	copy(buf[14:46], p.Hash[:])
	copy(buf[46:], p.UID)

	return buf, nil
}

func (c *TCPClient) Run(ctx context.Context, uidChan <-chan []byte) {
	var conn net.Conn
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case uid, ok := <-uidChan:
			if !ok {
				return
			}

			p := &UIDPacket{UID: uid}
			packetBytes, err := p.MarshalBinary()
			if err != nil {
				log.Printf("marshal err: %v", err)
				continue
			}

			// Write pump
			for {
				if ctx.Err() != nil {
					return
				}

				if conn == nil {
					var err error
					conn, err = net.DialTimeout("tcp", c.addr, c.dialTimeout)
					if err != nil {
						log.Printf("network err (dial): %v", err)
						time.Sleep(1 * time.Second)
						continue
					}
				}

				conn.SetWriteDeadline(time.Now().Add(c.dialTimeout))
				if _, err := conn.Write(packetBytes); err != nil {
					log.Printf("network err (write): %v", err)
					conn.Close()
					conn = nil
					continue
				}

				break
			}
		}
	}
}

func (c *TCPClient) Send(uid []byte) {
	conn, err := net.DialTimeout("tcp", c.addr, c.dialTimeout)
	if err != nil {
		log.Printf("network err (dial): %v", err)
		return
	}
	defer conn.Close()

	p := &UIDPacket{UID: uid}
	packetBytes, err := p.MarshalBinary()
	if err != nil {
		log.Printf("marshal err: %v", err)
		return
	}

	conn.SetWriteDeadline(time.Now().Add(c.dialTimeout))
	if _, err := conn.Write(packetBytes); err != nil {
		log.Printf("network err (write): %v", err)
		return
	}
}
