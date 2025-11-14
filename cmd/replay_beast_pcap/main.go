package main

import (
	"fmt"
	"net"
	"os"
	"time"

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
			Required: true,
		},
		&cli.StringFlag{
			Category: "PCAP",
			Name:     "pcap-beasthost",
			Required: true,
		},
		&cli.StringFlag{
			Category: "PCAP",
			Name:     "pcap-beastport",
			Required: true,
		},
		&cli.BoolFlag{
			Category: "PCAP",
			Name:     "no-sleep",
		},
		&cli.StringFlag{
			Category: "OUTPUT",
			Name:     "output-beasthost",
			Required: true,
		},
		&cli.StringFlag{
			Category: "OUTPUT",
			Name:     "output-beastport",
			Required: true,
		},
	}

	app.Action = runApp

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("exited with error")
	}
}

func runApp(c *cli.Context) error {

	// open connection to beast host
	beastOut := fmt.Sprintf("%s:%s", c.String("output-beasthost"), c.String("output-beastport"))
	log.Info().Msgf("Dialling: %s", beastOut)
	conn, err := net.Dial("tcp", beastOut)
	if err != nil {
		return fmt.Errorf("could not connect to %s: %v", beastOut, err)
	}
	defer func() {
		_ = conn.Close()
	}()
	log.Info().Msgf("Connected to: %s", beastOut)

	// open pcap file
	log.Info().Msgf("Opening: %s", c.String("pcap-file"))
	handle, err := pcap.OpenOffline(c.String("pcap-file"))
	if err != nil {
		return fmt.Errorf("could not open %s: %v", c.String("pcap-file"), err)
	}
	defer func() {
		handle.Close()
	}()

	// iterate through packets, playing back
	log.Info().Msg("Replaying packets")
	bytesWritten := 0
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	n := 0
	m := 0
	var prevPacketTime time.Time
	for packet := range packetSource.Packets() {
		fmt.Print(".")
		n++
		if n > 1 && !c.Bool("no-sleep") {
			// wait for time between packets unless no-sleep
			sleepTime := packet.Metadata().Timestamp.Sub(prevPacketTime)
			time.Sleep(sleepTime)
		}
		prevPacketTime = packet.Metadata().Timestamp

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

		m, err = conn.Write(packet.ApplicationLayer().Payload())
		if err != nil {
			return fmt.Errorf("could not write to %s: %v", beastOut, err)
		}

		bytesWritten += m

	}

	fmt.Print("\n")
	log.Info().Msgf("Finished writing %d bytes", bytesWritten)

	return nil
}
