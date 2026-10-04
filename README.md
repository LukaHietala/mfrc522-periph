This repository contains the reader for the [student tracking
app](https://github.com/LukaHietala/student-tracker).

## How to use

### Prerequisites:

- Enable SPI in `raspi-config`
- Make sure to be connected to the same network as main server
- Install Golang (=<1.27.0) 
- Wire MFRC522 module in and use the pins below to get it working with minimal
  fiddling

| Signal | Pin    | GPIO    |
|--------|--------|---------|
| 3.3V   | Pin 1  | Power   |
| GND    | Pin 6  | Ground  |
| IRQ    | Pin 18 | GPIO 24 |
| MISO   | Pin 21 | GPIO 9  |
| RST    | Pin 22 | GPIO 25 |
| SCK    | Pin 23 | GPIO 11 |
| SDA    | Pin 24 | GPIO 8  |
| MOSI   | Pin 19 | GPIO 10 |

> Many tutorials omit IRQ pin, but the periph library uses interrupts instead of
> polling to be more efficient

> If you decide to use other pins make sure to update the code in `main.go`

### Running

```
go build . 
./mfrc522
```

> `--help` to list out all flags and their usage.

 Once started the device advertises itself on current network via mDNS (`mdns.go`) under the service name
 "_rfid_reader._tcp" with label following the following pattern: `{label_flag}+{device_mac_address}`. The main
 server can discover these and pair with this device. Pair info is stored in "config.json".

To make sure the reader always starts up after reboot you should create a
Systemd service. After it has been created the logs can be read from the systemd journal

This reader has been tested on:
- Raspberry Pi 3 Model B+ Rev 1.3
- Mifare MFRC522 [Bus: SPI0.0, Reset Pin: GPIO25, IRQ Pin: GPIO24]

Other useful stuff:
- https://github.com/periph/devices/blob/main/mfrc522/example_test.go
- https://www.nxp.com/docs/en/data-sheet/MFRC522.pdf
- https://periph.io/device/mf-rc522/
- https://github.com/hrzlgnm/mdns-browser

Ports:
- 8080 - Pairing
- 8081 - Pinging

### Possible improvements

- Use MQTT instead of the custom binary protocol. This would make the code less
  flaky and more easier to extend
