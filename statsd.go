package gop

import (
	"strings"
	"sync"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
)

type StatsdClient struct {
	sync.Mutex
	client     statsd.Statter
	hostPort   string
	prefix     string
	rate       float32
	app        *App
	reconEvery time.Duration
	reconTimer *time.Timer
}

func (a *App) initStatsd() {
	a.configureStatsd(nil)
	a.Cfg.AddOnChangeCallback(a.configureStatsd)
}

func (a *App) configureStatsd(cfg *Config) {
	statsdHostport, _ := a.Cfg.Get("gop", "statsd_hostport", "localhost:8125")
	prefix := []string{}
	if p, ok := a.Cfg.Get("gop", "statsd_prefix", ""); ok {
		prefix = append(prefix, p)
	}
	hostname := strings.Replace(a.Hostname(), ".", "_", -1)
	prefix = append(prefix, a.ProjectName, a.AppName, hostname)
	statsdPrefix := strings.Join(prefix, ".")
	rate, _ := a.Cfg.GetFloat32("gop", "statsd_rate", 1.0)
	recon, _ := a.Cfg.GetDuration("gop", "statsd_reconnect_every", time.Minute*1)
	// TODO: Need to protect a.Stats from race
	a.Stats = StatsdClient{
		client:     nil,
		hostPort:   statsdHostport,
		prefix:     statsdPrefix,
		rate:       rate,
		app:        a,
		reconEvery: recon,
	}
	a.Infof("STATSD sending to [%s] with prefix [%s] at rate [%f]", a.Stats.hostPort, a.Stats.prefix, a.Stats.rate)
	a.Stats.connect()
}

// connect makes a 'connection' to the statsd host (doing a DNS lookup),
// setting the internal client. If that fails we set the internal client to
// noop. If reconEvery is set non 0, periodically re-connects on that duration.
func (s *StatsdClient) connect() bool {
	s.Lock()
	defer s.Unlock()
	if s.reconTimer != nil {
		s.reconTimer.Stop()
		s.reconTimer = nil
	}
	var err error
	s.client, err = statsd.New(s.hostPort, s.prefix)
	if err != nil {
		s.app.Error("STATSD Failed to create client (stats will noop): " + err.Error())
		s.client, err = statsd.NewNoopClient()
	} else {
		s.app.Finef("STATSD sending to [%s] with prefix [%s] at rate [%f]", s.hostPort, s.prefix, s.rate)
	}
	if s.reconEvery != 0 {
		s.reconTimer = time.AfterFunc(s.reconEvery, func() { s.connect() })
	}
	return true
}

func (s *StatsdClient) c() statsd.Statter {
	s.Lock()
	ptr := s.client
	s.Unlock()
	return ptr
}

func (s *StatsdClient) Dec(stat string, value int64) {
	s.app.Fine("STATSD DEC %s %d", stat, value)
	_ = s.c().Dec(stat, value, s.rate)
}

func (s *StatsdClient) Gauge(stat string, value int64) {
	s.app.Fine("STATSD GAUGE %s %d", stat, value)
	_ = s.c().Gauge(stat, value, s.rate)
}

func (s *StatsdClient) GaugeDelta(stat string, value int64) {
	s.app.Fine("STATSD GAUGEDELTA %s %d", stat, value)
	_ = s.c().GaugeDelta(stat, value, s.rate)
}

func (s *StatsdClient) Inc(stat string, value int64) {
	s.app.Fine("STATSD INC %s %d", stat, value)
	_ = s.c().Inc(stat, value, s.rate)
}

func (s *StatsdClient) Timing(stat string, delta int64) {
	s.app.Fine("STATSD TIMING %s %d", stat, delta)
	_ = s.c().Timing(stat, delta, s.rate)
}

func (s *StatsdClient) TimingDuration(stat string, delta time.Duration) {
	s.app.Fine("STATSD TIMING %s %s", stat, delta)
	_ = s.c().TimingDuration(stat, delta, s.rate)
}

// TimingTrack records the time something took to run using a defer, very good
// for timing a function call, just add a the defer at the top like so:
//     func timeMe() {
//         defer app.Stats.TimingTrack("timeMe.run_time", time.Now())
//         // rest of code, can return anywhere and run time tracked
//     }
// This works because the time.Now() is run when defer line is reached, so gets
// the time timeMe statred
func (s *StatsdClient) TimingTrack(stat string, start time.Time) {
	elapsed := time.Since(start)
	s.app.Finef("STATSD TIMING %s %s", stat, elapsed)
	_ = s.c().TimingDuration(stat, elapsed, s.rate)
}
