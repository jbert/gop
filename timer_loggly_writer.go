package gop

import (
	"fmt"

	"github.com/segmentio/go-loggly"
)

// A timber.LogWriter for the loggly service.

// LogglyWriter is a Timber writer to send logging to the loggly
// service. See: https://loggly.com.
type LogglyWriter struct {
	c *loggly.Client
}

// NewLogEntriesWriter creates a new writer for sending logging to logentries.
func NewLogglyWriter(token string, tags ...string) (*LogglyWriter, error) {
	return &LogglyWriter{c: loggly.New(token, tags...)}, nil
}

// LogWrite the message to the logenttries server async. Satifies the timber.LogWrite interface.
func (w *LogglyWriter) LogWrite(msg string) {
	// using type for the message string is how the Info etc methods on the
	// loggly client work.
	// TODO: Add a "level" key for info, error..., proper timestamp etc
	// Buffers the message for async send
	lmsg := loggly.Message{"type": msg}
	if err := w.c.Send(lmsg); err != nil {
		// TODO: What is best todo here as if we log it will loop?
		fmt.Println("loggly send error: %s", err.Error())
	}
}

// Close the write. Satifies the timber.LogWriter interface.
func (w *LogglyWriter) Close() {
	w.c.Flush()
}
