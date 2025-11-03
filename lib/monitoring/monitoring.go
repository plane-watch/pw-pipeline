package monitoring

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

type (
	HealthCheck interface {
		HealthCheckName() string
		HealthCheck() bool
	}
)

var (
	healthChecks     map[string]HealthCheck
	healthChecksLock sync.RWMutex
)

func init() {
	healthChecks = make(map[string]HealthCheck)
}

func IncludeMonitoringFlags(app *cli.App, defaultPort int) {
	app.Flags = append(app.Flags,
		&cli.IntFlag{
			Name:    "monitoring-port",
			Usage:   "Port to listen on for prometheus app metrics.",
			Value:   defaultPort,
			EnvVars: []string{"MONITORING_PORT"},
		},
		&cli.BoolFlag{
			Name:    "enable-net-pprof",
			Usage:   "Enable net pprof profiling at /debug/pprof",
			Value:   false,
			EnvVars: []string{"MONITORING_NET_PPROF"},
		},
	)
}

func RunWebServer(c *cli.Context) {
	go func() {
		monitoringPort := c.Int("monitoring-port")
		log.Info().Int("Port", monitoringPort).Msgf("Monitoring listener Listener")

		mux := http.NewServeMux()

		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/status", healthCheck)

		// Conditionally enable pprof endpoints
		if c.Bool("enable-net-pprof") {
			log.Info().Int("Port", monitoringPort).Msg("Enabling /debug/pprof endpoints")
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		}

		// TODO(MikeNye): Do we want to add error handling around this?
		_ = http.ListenAndServe(fmt.Sprintf(":%d", monitoringPort), mux)
	}()
}

func AddHealthCheck(f HealthCheck) {
	log.Debug().Str("name", f.HealthCheckName()).Msg("Adding Health Check")
	healthChecksLock.Lock()
	defer healthChecksLock.Unlock()
	healthChecks[f.HealthCheckName()] = f
}

func RemoveHealthCheck(f HealthCheck) {
	log.Debug().Str("name", f.HealthCheckName()).Msg("Removing Health Check")
	healthChecksLock.Lock()
	defer healthChecksLock.Unlock()
	delete(healthChecks, f.HealthCheckName())
}

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	healthChecksLock.RLock()
	defer healthChecksLock.RUnlock()
	healthy := len(healthChecks) > 0
	lgr := log.With().Str("Section", "Health Check").Logger()
	lgr.Debug().Int("Num Checks", len(healthChecks)).Msg("Performing Health Check")
	for _, check := range healthChecks {
		lgr.Debug().
			Str("Name", check.HealthCheckName()).
			Msg("Performing check...")
		ok := check.HealthCheck()
		lgr.Info().
			Str("Name", check.HealthCheckName()).
			Bool("Ok", ok).
			Msg("Performing returned...")
		if !ok {
			healthy = false
		}
	}

	if healthy {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "pass"}`))
	} else {
		lgr.Error().Msg("System is not Healthy")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status": "fail"}`))
	}
}
