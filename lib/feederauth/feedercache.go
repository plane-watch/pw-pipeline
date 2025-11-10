package feederauth

import (
	"context"
	"fmt"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
	"plane.watch/lib/export"
	"plane.watch/lib/icaoregion"
	"plane.watch/lib/nats_io"
	"plane.watch/lib/timing"
)

type (
	Protocol    uint8
	FeederCache struct {
		// feeders is a map containing all authorised feeders.
		// The map key is the feeder's API Key
		feeders map[string]export.Feeder

		// muFeeders is the mutex for Manifest.feeders
		muFeeders sync.RWMutex

		// feederRegion is a map containing a cache of feeder UUIDs and their region
		feederRegion map[string]icaoregion.Region

		// muFeederRegion is the mutex for Manifest.feederRegion
		muFeederRegion sync.RWMutex

		// feedersConnected map has a key for each connected feeder. The key is the feeder's api key.
		// This is used to limit the number of connections per feeder to one.
		feedersConnected map[string]map[Protocol]struct{}

		// muFeedersConnected is the mutex for Manifest.feedersConnected
		muFeedersConnected sync.RWMutex

		// feederConnectionTime map has a key for each connected feeder.
		// The key is the feeder's api key. The value is the last connection time.
		// This is used to limit the rate of connections per feeder to one per 30 sec.
		feederConnectionTime map[string]map[Protocol]time.Time

		// muFeederConnectionTime is the mutex for Manifest.feederConnectionTime
		muFeederConnectionTime sync.RWMutex

		natsURL    string
		natsServer *nats_io.Server

		log zerolog.Logger

		refresherCancelFunc context.CancelFunc

		locator *icaoregion.Locator
	}

	Option func(*FeederCache)
)

const (
	BEAST Protocol = iota
	MLAT
)

func WithLogger(log zerolog.Logger) Option {
	return func(f *FeederCache) {
		f.log = log.With().Str("Section", "FeederCache").Logger()
	}
}

func WithNatsURL(natsURL string) Option {
	return func(f *FeederCache) {
		f.natsURL = natsURL
	}
}

func New(opts ...Option) (*FeederCache, error) {
	var err error
	f := &FeederCache{}

	f.feeders = make(map[string]export.Feeder)
	f.feedersConnected = make(map[string]map[Protocol]struct{})
	f.feederConnectionTime = make(map[string]map[Protocol]time.Time)
	f.feederRegion = make(map[string]icaoregion.Region)

	f.locator, err = icaoregion.NewLocator()
	if err != nil {
		return nil, fmt.Errorf("error creating NewLocator: %v", err)
	}

	for _, opt := range opts {
		opt(f)
	}

	f.log.Info().Msg("Initializing FeederCache")

	// setup our nats connection
	// sanity checks
	if f.natsURL == "" {
		return nil, fmt.Errorf("WithNatsURL option is required")
	}
	f.natsServer, err = nats_io.NewServer(
		nats_io.WithConnections(false, true),
		nats_io.WithServer(f.natsURL, "runway-atc-client-feeders"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup nats connection: %w", err)
	}

	// initial fetch feeders
	err = f.fetchFeeders()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feeders: %w", err)
	}

	// start feeder data refresher
	f.refresherCancelFunc = timing.RunOnTicker(f.log, time.Minute, f.fetchFeeders)

	return f, nil
}

func (f *FeederCache) fetchFeeders() error {
	f.log.Debug().Msg("fetching feeders")
	ret, err := f.natsServer.Request(export.NatsApiFeederListV1, nil, map[string]string{}, time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch feeder list from atc api: %w", err)
	}
	json := jsoniter.ConfigFastest

	feeders := make(export.Feeders, 0, len(f.feeders))

	err = json.Unmarshal(ret, &feeders)
	if err != nil {
		return fmt.Errorf("failed to decode feeder list: %w", err)
	}

	f.log.Info().
		Int("prev-feeder-count", len(f.feeders)).
		Int("new-feeder-count", len(feeders)).
		Msg("Updated Authorised Feeders")

	f.populate(&feeders)

	return nil
}

func (f *FeederCache) Reset(p Protocol) {
	f.muFeedersConnected.Lock()
	defer f.muFeedersConnected.Unlock()

	for apiKey := range f.feedersConnected {
		if _, ok := f.feedersConnected[apiKey]; ok {
			if _, ok := f.feedersConnected[apiKey][p]; ok {
				delete(f.feedersConnected[apiKey], p)
			}
		}
	}
}

func (f *FeederCache) IsValid(apiKey string) bool {
	f.muFeeders.RLock()
	defer f.muFeeders.RUnlock()
	_, ok := f.feeders[apiKey]
	return ok
}

func (f *FeederCache) IsConnected(apiKey string, p Protocol) bool {
	f.muFeedersConnected.RLock()
	defer f.muFeedersConnected.RUnlock()
	if _, ok := f.feedersConnected[apiKey]; ok {
		if _, ok := f.feedersConnected[apiKey][p]; ok {
			return true
		}
	}
	return false
}

func (f *FeederCache) IsConnectingTooFrequently(apiKey string, p Protocol) bool {
	f.muFeederConnectionTime.RLock()
	defer f.muFeederConnectionTime.RUnlock()

	if _, ok := f.feederConnectionTime[apiKey]; ok {
		if lastConnTime, ok := f.feederConnectionTime[apiKey][p]; ok && lastConnTime.After(time.Now().Add(-30*time.Second)) {
			return true
		}
	}

	return false
}

func (f *FeederCache) Authenticate(apiKey string, p Protocol) (bool, error) {

	// check api key valid
	if !f.IsValid(apiKey) {
		return false, fmt.Errorf("feeder not found")
	}

	// check if already connected
	if f.IsConnected(apiKey, p) {
		return false, fmt.Errorf("feeder already connected")
	}

	// check for too frequent connections (one connection per 30 seconds)
	if f.IsConnectingTooFrequently(apiKey, p) {
		return false, fmt.Errorf("feeder connecting too frequently")
	}

	return true, nil
}

func (f *FeederCache) Close() error {
	if f != nil {
		f.refresherCancelFunc()
	}
	return nil
}

func (f *FeederCache) Get(apiKey string) (export.Feeder, error) {
	f.muFeeders.RLock()
	defer f.muFeeders.RUnlock()
	feeder, ok := f.feeders[apiKey]
	if !ok {
		return export.Feeder{}, fmt.Errorf("feeder %s not found", apiKey)
	}
	return feeder, nil
}

func (f *FeederCache) Region(apiKey string) (icaoregion.Region, error) {

	// get feeder for lat & lon
	feeder, err := f.Get(apiKey)
	if err != nil {
		return icaoregion.Unknown, fmt.Errorf("failed to get feeder %s: %w", apiKey, err)
	}

	// get region if possible
	f.muFeederRegion.RLock()
	r, ok := f.feederRegion[apiKey]
	f.muFeederRegion.RUnlock()

	// if not possible, look up & set region
	if !ok {
		r = f.locator.RegionOfLatLon(*feeder.Latitude, *feeder.Longitude)
		f.muFeederRegion.Lock()
		f.feederRegion[apiKey] = r
		f.muFeederRegion.Unlock()
	}

	return r, nil
}

func (f *FeederCache) populate(feeders *export.Feeders) {

	f.muFeeders.Lock()
	defer f.muFeeders.Unlock()
	f.muFeedersConnected.Lock()
	defer f.muFeedersConnected.Unlock()
	f.muFeederConnectionTime.Lock()
	defer f.muFeederConnectionTime.Unlock()

	clear(f.feeders) // keeps capacity, prevent unnecessary alloc

	// update feeders from nats output
	for _, feeder := range *feeders {
		f.feeders[feeder.ApiKey.String()] = feeder
	}

	// clean-up
	f.cleanup()
}

func (f *FeederCache) cleanup() {
	for apikey := range f.feedersConnected {
		if len(f.feedersConnected[apikey]) == 0 {
			delete(f.feedersConnected, apikey)
		}
	}
	for apiKey := range f.feederConnectionTime {
		for p := range f.feederConnectionTime[apiKey] {
			if f.feederConnectionTime[apiKey][p].Before(time.Now().Add(-time.Minute)) {
				delete(f.feederConnectionTime[apiKey], p)
			}
		}
		if len(f.feederConnectionTime[apiKey]) == 0 {
			delete(f.feederConnectionTime, apiKey)
		}
	}
}

func (f *FeederCache) SetConnected(apiKey string, p Protocol) {
	f.muFeederConnectionTime.Lock()
	defer f.muFeederConnectionTime.Unlock()
	f.muFeedersConnected.Lock()
	defer f.muFeedersConnected.Unlock()

	if _, ok := f.feederConnectionTime[apiKey]; !ok {
		f.feederConnectionTime[apiKey] = make(map[Protocol]time.Time)
	}
	f.feederConnectionTime[apiKey][p] = time.Now()

	if _, ok := f.feedersConnected[apiKey]; !ok {
		f.feedersConnected[apiKey] = make(map[Protocol]struct{})
	}
	f.feedersConnected[apiKey][p] = struct{}{}
}

func (f *FeederCache) SetDisconnected(apiKey string, p Protocol) {
	f.muFeedersConnected.Lock()
	defer f.muFeedersConnected.Unlock()
	delete(f.feedersConnected[apiKey], p)
}
