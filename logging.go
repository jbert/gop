package gop

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cocoonlife/timber"
)

type LogFormatterFactory interface {
	Create() timber.LogFormatter
}

type TimberLogFormatterFactory struct {
}

func (t *TimberLogFormatterFactory) Create() timber.LogFormatter {
	return timber.NewJSONFormatter()
}

type Logger timber.Logger

func string2Level(logLevelStr string) (timber.Level, error) {
	logLevelStr = strings.ToUpper(logLevelStr)
	for logLevel, levelStr := range timber.LongLevelStrings {
		if logLevelStr == levelStr {
			return timber.Level(logLevel), nil
		}
	}
	return 0, errors.New("Not found")
}

func (a *App) makeConfigLogger() (timber.ConfigLogger, bool) {
	defaultLogPattern := "[%D %T] [%L] %M"
	filenamesByDefault, _ := a.Cfg.GetBool("gop", "log_filename", false)
	if filenamesByDefault {
		defaultLogPattern = "[%D %T] [%L] %s %M"
	}
	logPattern, _ := a.Cfg.Get("gop", "log_pattern", defaultLogPattern)

	// If set, hack all logging to stdout for dev
	forceStdout, _ := a.Cfg.GetBool("gop", "stdout_only_logging", false)
	configLogger := timber.ConfigLogger{
		LogWriter: new(timber.ConsoleWriter),
		Level:     timber.INFO,
		Formatter: timber.NewPatFormatter(logPattern),
	}

	defaultLogDir, _ := a.Cfg.Get("gop", "log_dir", "/var/log")
	fellbackToCWD := false
	a.logDir = defaultLogDir + "/" + a.ProjectName
	if !forceStdout {
		defaultLogFname := a.logDir + "/" + a.AppName + ".log"
		logFname, _ := a.Cfg.Get("gop", "log_file", defaultLogFname)

		_, dirExistsErr := os.Stat(a.logDir)
		if dirExistsErr != nil && os.IsNotExist(dirExistsErr) {
			// Carry on with stdout logging, but remember to mention it
			fellbackToCWD = true
			a.logDir = "."
		} else {
			newWriter, err := timber.NewFileWriter(logFname)
			if err != nil {
				panic(fmt.Sprintf("Can't open log file: %s", err))
			}
			configLogger.LogWriter = newWriter
		}
	}

	logLevelStr, _ := a.Cfg.Get("gop", "log_level", "INFO")
	logLevel, err := string2Level(logLevelStr)
	if err == nil {
		configLogger.Level = timber.Level(logLevel)
	}

	granularsPrefix, _ := a.Cfg.Get("gop", "log_granulars_prefix", "")
	granularsStrs, _ := a.Cfg.GetList("gop", "log_granulars", nil)
	if granularsStrs != nil {
		configLogger.Granulars = make(map[string]timber.Level)
	GRANULARS:
		for _, granStr := range granularsStrs {
			bits := strings.Split(granStr, ":")
			if len(bits) != 2 {
				continue GRANULARS
			}
			pkgPart := bits[0]
			pkgLevel := bits[1]

			if pkgPart == "" || pkgLevel == "" {
				continue GRANULARS
			}
			pkgName := pkgPart
			if granularsPrefix != "" {
				pkgName = granularsPrefix + "/" + pkgPart
			}
			logLevel, err := string2Level(pkgLevel)
			if err == nil {
				configLogger.Granulars[pkgName] = logLevel
			}
		}
	}

	return configLogger, fellbackToCWD
}

func (a *App) setLogger(name string, logger timber.ConfigLogger) {
	l := timber.Global
	if i, ok := a.loggerMap[name]; ok {
		l.SetLogger(i, logger)
	} else {
		a.loggerMap[name] = l.AddLogger(logger)
	}
}

func (a *App) initLogging(extraTags ...string) {
	// *Don't* create a NewTImber here. Logs are only flushed on Close() and if we
	// have more than one timber, it's easy to only Close() one of them...
	l := timber.Global
	a.Logger = l

	// Set up the default go logger to go here too, so 3rd party
	// module logging plays nicely
	log.SetFlags(0)
	log.SetOutput(l)

	a.configureLogging(extraTags)
	a.Cfg.AddOnChangeCallback(func(cfg *Config) { a.configureLogging() })
}

func (a *App) configureLogging(extraTags ...string) {
	l := timber.Global

	configLogger, fellbackToCWD := a.makeConfigLogger()
	a.setLogger("configLogger", configLogger)
	if fellbackToCWD {
		l.Error("Logging directory does not exist - logging to stdout")
	}

	doAccessLog, _ := a.Cfg.GetBool("gop", "access_log_enable", false)
	if doAccessLog {
		defaultAccessLogFname := a.logDir + "/" + a.AppName + "-access.log"
		accessLogFilename, _ := a.Cfg.Get("gop", "access_log_filename", defaultAccessLogFname)
		// Don't use .Create since it truncates
		var err error
		a.accessLog, err = os.OpenFile(accessLogFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			l.Errorf("Can't open access log; %s", err.Error())
		}
	}

	// Loggly logging service
	token, haveToken := a.Cfg.Get("gop", "log_loggly_token", "")
	if haveToken {
		level, _ := a.Cfg.Get("gop", "log_level", "info")
		// ToUpper used because that's how timber expects the levels
		// to be written
		level = strings.ToUpper(level)
		tags := []string{a.ProjectName, a.AppName, a.Hostname()}
		tags = append(tags, extraTags...)
		if lw, err := NewLogglyWriter(token, tags...); err == nil {
			logger := timber.ConfigLogger{
				LogWriter: lw,
				Level:     timber.GetLevel(level),
				Formatter: a.logFormatterFactory.Create(),
			}
			a.setLogger("loggly", logger)
			l.Infof("Added Loggly logger with tags:%s", tags)
		} else {
			l.Errorf("Error creating loggly client: %s", err.Error())
		}
	}
}

func (a *App) closeLogging() {
	if a.accessLog != nil {
		err := a.accessLog.Close()
		if err != nil {
			a.Errorf("Error closing access log: %s", err.Error())
		}
	}
	timber.Close()
}

func (a *App) WriteAccessLog(req *Req, dur time.Duration) {
	if a.accessLog == nil {
		return
	}
	logEvery, _ := a.Cfg.GetInt("gop", "access_log_every", 0)
	if logEvery > 0 {
		a.suppressedAccessLogLines++
		if a.suppressedAccessLogLines < logEvery {
			a.Debug("Suppressing access log line [%d/%d]", a.suppressedAccessLogLines, logEvery)
			return
		}
	}
	a.suppressedAccessLogLines = 0

	// Copy an nginx-log access log
	/* ---
	   gaiadev.leedsdev.net 0.022 192.168.111.1 - - [05/Feb/2014:13:39:22 +0000] "GET /bby/sso/login?next_url=https%3A%2F%2Fgaiadev.leedsdev.net%2F HTTP/1.1" 302 0 "-" "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:26.0) Gecko/20100101 Firefox/26.0"
	   --- */
	trimPort := func(s string) string {
		colonOffset := strings.IndexByte(s, ':')
		if colonOffset >= 0 {
			s = s[:colonOffset]
		}
		return s
	}
	quote := func(s string) string {
		return string(strconv.AppendQuote([]byte{}, s))
	}

	reqFirstLine := fmt.Sprintf("%s %s %s", req.R.Method, req.R.RequestURI, req.R.Proto)
	referrerLine := req.R.Referer()
	if referrerLine == "" {
		referrerLine = "-"
	}
	uaLine := req.R.Header.Get("User-Agent")
	if uaLine == "" {
		uaLine = "-"
	}
	var size, code int
	if req.W != nil {
		code = req.W.code
		size = req.W.size
	}
	hostname := a.Hostname()
	logLine := fmt.Sprintf("%s %.3f %s %s %s %s %s %d %d %s %s\n",
		hostname,
		dur.Seconds(),
		trimPort(req.RealRemoteIP),
		"-", // Ident <giggle>
		"-", // user
		//		req.startTime.Format("[02/Jan/2006:15:04:05 -0700]"),
		req.startTime.Format("["+time.RFC3339+"]"),
		quote(reqFirstLine),
		code,
		size,
		quote(referrerLine),
		quote(uaLine))
	_, err := req.app.accessLog.WriteString(logLine)
	if err != nil {
		a.Errorf("Failed to write to access log: %s", err.Error())
	}
}
