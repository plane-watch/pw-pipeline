package producer

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/timing"
	"plane.watch/lib/tracker"
)

const (
	cmdExit = 1

	Avr = iota
	Beast
	Sbs1
)

type (
	Producer struct {
		tracker.FrameSource
		producerType int

		log zerolog.Logger

		out chan tracker.FrameEvent

		cmdChan chan int

		splitter bufio.SplitFunc

		hasFetcher       bool
		fetcherConnected bool

		beastDelay        bool
		keepAliveRepeater bool
		isRadarCape       bool

		running bool
		run     func()

		runningLock sync.Mutex

		stats struct {
			avr, beast, sbs1 prometheus.Counter
		}

		// MLAT epoch tracking for Beast frames
		mlatEpoch     time.Duration // First MLAT tick value seen
		wallEpoch     time.Time     // Wall time when first frame arrived
		lastMlatTicks time.Duration // Last MLAT ticks seen (for reset detection)
		hasEpoch      bool          // Whether epoch has been established

		// Metrics for epoch tracking
		epochResets      prometheus.Counter
		driftCorrections prometheus.Counter

		// Observability gauges for RTT and drift analysis
		rttGauge   prometheus.Gauge // TCP RTT for this connection
		driftGauge prometheus.Gauge // Current drift (arrival - calculated time)

		repeater *keepAliveRepeater

		poisonPill       func() bool
		poisonPillCancel context.CancelFunc

		cleanUpTasks []func() error
	}

	Option func(*Producer)
)

func New(opts ...Option) *Producer {
	p := &Producer{
		FrameSource: tracker.FrameSource{
			OriginIdentifier: "",
			Name:             "",
			Tag:              "",
			RefLat:           nil,
			RefLon:           nil,
			VelocityCheck:    true,
		},
		out:     make(chan tracker.FrameEvent, 100),
		cmdChan: make(chan int),
		run: func() {
			println("You did not specify any sources")
			os.Exit(1) // TODO(mikenye): something more graceful?
		},
		cleanUpTasks: make([]func() error, 0),
	}
	p.log = log.With().Logger()

	for _, opt := range opts {
		opt(p)
	}

	if "" == p.Name {
		p.Name = p.OriginIdentifier
	}
	if "" == p.Name {
		p.Name = p.Tag
	}
	if "" == p.Name {
		p.Name = producerType(p.producerType)
	}
	p.log = log.With().
		Str("Name", p.Name).
		Str("ProducerType", producerType(p.producerType)).
		Logger()

	if p.keepAliveRepeater {
		p.log.Debug().Msg("Setting up repeater")
		p.repeater = newKeepAliveRepeater()
		go p.repeater.processor(p)
	}

	if p.poisonPill != nil {
		p.poisonPillCancel = timing.RunOnTicker(p.log, time.Second*5, func() error {
			if p.poisonPill() {
				log.Debug().Msg("took poison pill")
				p.Stop()
			}
			return nil
		})
	}

	return p
}

func producerType(in int) string {
	switch in {
	case Avr:
		return "AVR"
	case Beast:
		return "Beast"
	case Sbs1:
		return "SBS1"
	default:
		return "Unknown"
	}
}

// Producer.New(WithFetcher(host, port), WithType(Producer.Avr), WithRefLatLon(lat, lon))

func WithCleanUpTasks(tasks ...func() error) Option {
	return func(p *Producer) {
		p.cleanUpTasks = append(p.cleanUpTasks, tasks...)
	}
}

func WithListener(host, port string) Option {
	return func(p *Producer) {
		p.addDebug("configuring listener")
		p.run = func() {
			p.addDebug("about to start listening")
			defer p.Cleanup()

			addr := net.JoinHostPort(host, port)
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				// handle error
				p.log.Error().Err(err).Str("host:port", addr).Msg("Failed to listen")
				return
			}
			p.addDebug("here we go, listening")
			go func() {
				for cmd := range p.cmdChan {
					switch cmd {
					case cmdExit:
						p.log.Debug().Msg("Exiting...")
						_ = ln.Close()
						return
					}
				}
			}()
			for {
				p.addDebug("Top of listener loop")
				conn, errConn := ln.Accept()
				if errConn != nil {
					// handle error
					if errors.Is(errConn, net.ErrClosed) {
						p.log.Info().Msg("Closed Network Connection")
						break
					}

					p.log.Error().Err(errConn).Msg("Failed to accept a connection")
					continue
				}
				p.addDebug("After Accept")

				go func(c net.Conn) {
					p.addDebug("handling conn")
					scan := bufio.NewScanner(c)
					scan.Split(p.splitter)
					errRead := p.readFromScanner(scan)
					if nil != errRead {
						p.log.Error().Err(errRead).Msg("No more reading")
					}
					_ = c.Close()
				}(conn)
			}

			p.addInfo("Listener Closing")
		}
	}
}

func WithSourceTag(tag string) Option {
	return func(p *Producer) {
		p.FrameSource.Tag = tag
	}
}

func WithFetcher(host, port string) Option {
	hp := net.JoinHostPort(host, port)
	return func(p *Producer) {
		p.addDebug("configuring fetcher %s:%s", host, port)
		p.hasFetcher = true
		p.FrameSource.OriginIdentifier = hp
		p.run = func() {
			defer p.Cleanup()

			p.addInfo("Fetching From Host: %s:%s", host, port)
			p.fetcher(host, port, func(conn net.Conn) error {
				scan := bufio.NewScanner(conn)
				scan.Split(p.splitter)
				return p.readFromScanner(scan)
			})
		}
	}
}

func WithConnection(conn net.Conn) Option {
	return func(p *Producer) {
		p.FrameSource.OriginIdentifier = conn.RemoteAddr().String()
		p.run = func() {
			p.addInfo("Fetching From Host: %s", p.FrameSource.OriginIdentifier)
			go func() {
				defer p.Cleanup()

				defer func() {
					p.log.Debug().Msg("closing connection")
					_ = conn.Close()
				}()

				scan := bufio.NewScanner(conn)
				p.log.Debug().Msg("start reading from scanner")
				errRead := p.readFromScanner(scan)
				if errRead != nil {
					p.log.Error().Err(errRead).Msg("error reading from scanner")
				}
				_ = conn.Close()
				p.log.Debug().Msg("finish reading from scanner")
			}()
		}
	}
}

func WithOriginName(name string) Option {
	return func(p *Producer) {
		p.FrameSource.Name = name
	}
}

func WithFiles(filePaths []string) Option {
	return func(p *Producer) {
		p.FrameSource.VelocityCheck = p.beastDelay
		p.run = func() {
			// note: we do cleanup in readFiles so the producer doesn't close

			p.readFiles(filePaths, func(reader io.Reader, fileName string) error {
				scanner := bufio.NewScanner(reader)
				p.FrameSource.OriginIdentifier = "file://" + fileName
				return p.readFromScanner(scanner)
			})
		}
	}
}

func WithBeastDelay(beastDelay bool, isRadarCape bool) Option {
	return func(p *Producer) {
		p.beastDelay = beastDelay
		p.isRadarCape = isRadarCape
	}
}

func WithType(producerType int) Option {
	return func(p *Producer) {
		switch producerType {
		case Avr, Sbs1:
			p.producerType = producerType
			p.splitter = bufio.ScanLines
		case Beast:
			p.producerType = producerType
			p.splitter = ScanBeast
		default:
			p.log.Error().Msgf("Unknown Producer Type")
		}
	}
}

func WithPrometheusCounters(avr, beast, sbs1 prometheus.Counter) Option {
	return func(p *Producer) {
		p.stats.avr = avr
		p.stats.beast = beast
		p.stats.sbs1 = sbs1
	}
}

func WithEpochMetrics(epochResets, driftCorrections prometheus.Counter) Option {
	return func(p *Producer) {
		p.epochResets = epochResets
		p.driftCorrections = driftCorrections
	}
}

func WithRTTGauge(g prometheus.Gauge) Option {
	return func(p *Producer) {
		p.rttGauge = g
	}
}

func WithDriftGauge(g prometheus.Gauge) Option {
	return func(p *Producer) {
		p.driftGauge = g
	}
}

func (p *Producer) Source() *tracker.FrameSource {
	return &p.FrameSource
}

func (p *Producer) readFromScanner(scan *bufio.Scanner) error {
	scan.Split(p.splitter)

	switch p.producerType {
	case Avr:
		p.log = p.log.With().Str("type", "avr").Logger()
		return p.avrScanner(scan)
	case Sbs1:
		p.log = p.log.With().Str("type", "sbs1").Logger()
		return p.sbsScanner(scan)
	case Beast:
		p.log = p.log.With().Str("type", "beast").Logger()
		return p.beastScanner(scan)
	default:
		return errors.New("unknown Producer type")
	}
}

// WithReferenceLatLon sets up the reference lat/lon for decoding surface position messages
func WithReferenceLatLon(lat, lon *float64) Option {
	return func(p *Producer) {
		if lat != nil && lon != nil {
			p.log.Debug().Float64("lat", *lat).Float64("lon", *lon).Msg("With Reference Lat/Lon")
			p.FrameSource.RefLat = lat
			p.FrameSource.RefLon = lon
		}
	}
}
func WithKeepAliveRepeater() Option {
	return func(p *Producer) {
		p.keepAliveRepeater = true
	}
}

func WithPoisonPill(poisonPill func() bool, t time.Duration) Option {
	return func(p *Producer) {
		p.poisonPill = poisonPill
	}
}

func (p *Producer) String() string {
	return p.FrameSource.Name
}

func (p *Producer) Listen() chan tracker.FrameEvent {
	p.log.Debug().Msg("Producer starting operations")
	p.runningLock.Lock()
	defer p.runningLock.Unlock()
	if !p.running {
		go p.run()
		p.running = true
	}
	return p.out
}

func (p *Producer) addFrame(f tracker.Frame, s *tracker.FrameSource) {
	fe := tracker.NewFrameEvent(f, s)
	if p.keepAliveRepeater {
		// update the repeater for this listFrames
		p.repeater.chanFrame <- fe
	}
	p.AddEvent(fe)
}

func (p *Producer) addDebug(sfmt string, v ...interface{}) {
	p.log.Debug().
		Str("Section", "Producer").
		Str("frame-source", p.FrameSource.Name).
		Msgf(sfmt, v...)
}

func (p *Producer) addInfo(sfmt string, v ...interface{}) {
	p.log.Info().
		Str("Section", "Producer").
		Str("frame-source", p.FrameSource.Name).
		Msgf(sfmt, v...)
}

func (p *Producer) addError(err error) {
	p.log.Error().
		Str("Section", "Producer").
		Str("frame-source", p.FrameSource.Name).
		Err(err).
		Send()
}

func (p *Producer) HealthCheck() bool {
	if p.hasFetcher {
		return p.fetcherConnected
	}
	return true
}

func (p *Producer) HealthCheckName() string {
	return p.Name
}

func (p *Producer) Stop() {
	p.cmdChan <- cmdExit
}

func (p *Producer) AddEvent(e tracker.FrameEvent) {
	defer func() {
		if r := recover(); nil != r {
			p.log.Error().Interface("recover", r).Msg("Failed to add event")
		}
	}()
	p.out <- e
}

func (p *Producer) Cleanup() {
	p.log.Debug().Msg("Start Cleanup")

	// if using poison pill, then make sure the RunOnTicker instance is cancelled
	if p.poisonPillCancel != nil {
		p.poisonPillCancel()
	}

	// run user-defined clean-up functions
	for _, cleanUpFunc := range p.cleanUpTasks {
		err := cleanUpFunc()
		if err != nil {
			p.log.Error().Err(err).Msg("error in user-defined clean-up function")
		}
	}

	defer func() {
		if r := recover(); nil != r {
			p.log.Error().Interface("recover", r).Msg("Cleanup() had a panic")
		} else {
			p.log.Debug().Msg("Finished Cleanup")
		}
	}()

	close(p.out)
}

func (p *Producer) readFiles(dataFiles []string, read func(io.Reader, string) error) {
	var err error
	var inFile *os.File
	var gzipFile *gzip.Reader
	go func() {
		defer p.Cleanup()

		for _, inFileName := range dataFiles {
			log.Debug().Str("FileName", inFileName).Msg("Loading contents...")
			p.FrameSource.OriginIdentifier = "file://" + inFileName
			inFile, err = os.Open(inFileName)
			if err != nil {
				p.addError(fmt.Errorf("failed to open file {%s}: %s", inFileName, err))
				continue
			}

			isGzip := strings.ToLower(inFileName[len(inFileName)-2:]) == "gz"
			isBzip2 := strings.ToLower(inFileName[len(inFileName)-3:]) == "bz2"
			log.Debug().
				Str("FileName", inFileName).
				Bool("Is Gzip", isGzip).
				Bool("Is Bzip2", isBzip2).
				Bool("Is Plain", !isBzip2 && !isGzip).
				Msg("Format")

			if isGzip {
				gzipFile, err = gzip.NewReader(inFile)
				if nil != err {
					log.Error().Err(err).Str("file", inFileName).Msg("Failed to open file")
				}
				err = read(gzipFile, inFileName)
			} else if isBzip2 {
				bzip2File := bzip2.NewReader(inFile)
				err = read(bzip2File, inFileName)
			} else {
				err = read(inFile, inFileName)
			}
			if nil != err {
				p.addError(err)
			}
			_ = inFile.Close()
			log.Debug().
				Str("FileName", inFileName).
				Msg("Finished with file")
		}
		log.Debug().Msg("Done loading contents from files")
	}()

	go func() {
		for cmd := range p.cmdChan {
			switch cmd {
			case cmdExit:
				return
			}
		}
	}()
}

func (p *Producer) fetcher(host, port string, read func(net.Conn) error) {
	var conn net.Conn
	var wLock sync.RWMutex
	working := true

	isWorking := func() bool {
		wLock.RLock()
		defer wLock.RUnlock()
		return working
	}

	go func() {
		var backOff = time.Second
		var err error
		for isWorking() {
			p.addDebug("Connecting...")
			wLock.Lock()
			conn, err = net.Dial("tcp", net.JoinHostPort(host, port))
			wLock.Unlock()
			if nil != err {
				p.fetcherConnected = false
				p.addError(err)
				time.Sleep(backOff)
				backOff = backOff*2 + ((time.Duration(rand.Intn(20)) * time.Millisecond * 100) - time.Second)
				if backOff > time.Minute {
					backOff = time.Minute
				}
				continue
			}
			p.addDebug("Connected!")
			backOff = time.Second
			p.fetcherConnected = true

			if err = read(conn); nil != err {
				p.addError(err)
			}
		}
		p.addDebug("Done with Producer %s", p)
	}()

	for cmd := range p.cmdChan {
		p.addDebug("Got a Command... %d", cmd)
		switch cmd {
		case cmdExit:
			p.addDebug("Got Cmd Exit")
			wLock.Lock()
			working = false
			if conn != nil {
				if err := conn.Close(); err != nil {
					p.log.Error().Err(err).Msg("Err when closing socket")
				}
			}
			wLock.Unlock()
			return
		}
	}
}
