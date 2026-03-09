package sink

import (
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	QueueLocationUpdates = "location-updates"
)

type (
	Config struct {
		host, port string
		secure     bool

		vhost      string
		user, pass string
		queue      map[string]string

		waiter sync.WaitGroup

		sourceTag      string
		connectionName string

		stats struct {
			frame, planeLoc prometheus.Counter
		}

		sendDelay time.Duration

		// for remembering if we have recently sent this message
	}

	Option func(*Config)
)

func (c *Config) setupConfig(opts []Option) {
	c.sendDelay = 300 * time.Millisecond

	c.queue = map[string]string{}
	for _, opt := range opts {
		opt(c)
	}
}

func WithConnectionName(name string) Option {
	return func(conf *Config) {
		conf.connectionName = name
	}
}
func WithHost(host, port string) Option {
	return func(conf *Config) {
		conf.host = host
		conf.port = port
	}
}
func WithUserPass(user, pass string) Option {
	return func(conf *Config) {
		conf.user = user
		conf.pass = pass
	}
}

func WithSourceTag(tag string) Option {
	return func(config *Config) {
		config.sourceTag = tag
	}
}

func WithLogFile(file string) Option {
	return func(config *Config) {
		f, err := os.Create(file)
		if nil != err {
			println("Cannot open file: ", file)
			return
		}
		log.Logger = zerolog.New(f).With().Timestamp().Logger()
	}
}

func WithPrometheusCounters(frame, planeLoc prometheus.Counter) Option {
	return func(conf *Config) {
		conf.stats.frame = frame
		conf.stats.planeLoc = planeLoc
	}
}

func (c *Config) Finish() {
	c.waiter.Wait()
}

func WithSendDelay(delay time.Duration) Option {
	return func(conf *Config) {
		conf.sendDelay = delay
	}
}
