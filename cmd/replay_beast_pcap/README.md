# replay_beast_pcap

This utility will "replay" the BEAST data from a packet capture.

## Taking the capture

```bash
sudo tcpdump -i $IFACE -n src $BEASTHOST and src port $BEASTPORT -w beast.pcap
```

## Replaying

Start a readsb session with:

```bash
readsb --interactive --net --net-bi-port=30105 --net-only
```

... then replay the capture:

```bash
./replay_beast_pcap --pcap-file beast.pcap --pcap-beasthost $BEASTHOST --pcap-beastport $BEASTPORT --output-beasthost 127.0.0.1 --output-beastport 30105
```