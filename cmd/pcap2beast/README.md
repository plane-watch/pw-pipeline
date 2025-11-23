# pcap2beast

This utility will extract the BEAST data from a packet capture.

## Taking the capture

```bash
sudo tcpdump -i $IFACE -n src $BEASTHOST and src port $BEASTPORT -w beast.pcap
```


... then extract the BEAST data from the capture:

```bash
cat beast.pcap | pcap2beast --pcap-beasthost $BEASTHOST --pcap-beastport $BEASTPORT > data.beast
```

... then ingest the data with `pw_ingest`:

```bash
pw_ingest --file=beast:///path/to/data.beast --sink=nats://localhost:4222 simple
```