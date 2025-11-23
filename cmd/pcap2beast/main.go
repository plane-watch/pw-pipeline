package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func main() {

	app := cli.NewApp()
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Category: "PCAP",
			Name:     "pcap-file",
			Required: false,
			Value:    "-",
			Usage:    "pcap file to read. By default, read from stdin",
		},
		&cli.StringFlag{
			Category: "PCAP",
			Name:     "pcap-beasthost",
			Required: true,
			Usage:    "IP address of BEAST host in pcap file",
		},
		&cli.StringFlag{
			Category: "PCAP",
			Name:     "pcap-beastport",
			Required: true,
			Usage:    "TCP port of BEAST host in pcap file",
		},
	}

	app.Action = runApp

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("exited with error")
	}
}

func runApp(c *cli.Context) error {

	// open pcap file
	if c.String("pcap-file") != "-" {
		log.Info().Msgf("Opening: %s", c.String("pcap-file"))
	} else {
		log.Info().Msg("Opening standard input")
	}

	handle, err := pcap.OpenOffline(c.String("pcap-file"))
	if err != nil {
		return fmt.Errorf("could not open %s: %v", c.String("pcap-file"), err)
	}
	defer func() {
		handle.Close()
	}()

	// iterate through packets, playing back
	log.Info().Msgf("Extracting BEAST data from %s:%s", c.String("pcap-beasthost"), c.String("pcap-beastport"))
	bytesWritten := 0
	frameCount := 0
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {

		// skip packets that aren't from the pcap-beasthost and pcap-beastport
		if packet.LinkLayer().LayerType() != layers.LayerTypeEthernet {
			continue
		}
		if packet.NetworkLayer().LayerType() != layers.LayerTypeIPv4 {
			continue
		}
		if packet.TransportLayer().LayerType() != layers.LayerTypeTCP {
			continue
		}
		if packet.NetworkLayer().NetworkFlow().Src().String() != c.String("pcap-beasthost") {
			continue
		}
		if packet.TransportLayer().TransportFlow().Src().String() != c.String("pcap-beastport") {
			continue
		}

		payload := packet.ApplicationLayer().Payload()
		fmt.Println(hex.EncodeToString(payload))
		bytesWritten += len(payload)
		frameCount++
	}

	log.Info().Msgf("Finished writing %d bytes from %d ethernet frames", bytesWritten, frameCount)

	return nil
}
