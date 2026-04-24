// Package logging wires zerolog as the project-wide structured logger.
//
// Output goes to stderr (keeps stdout clean for MCP JSON-RPC). Pretty
// console output in TTY, JSON otherwise. Use Init once at startup.
package logging

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init configures the global zerolog logger.
// verbose=true → debug level. Env SRCMAP_LOG=trace|debug|info|warn|error overrides.
// jsonOut=true forces JSON (for MCP / machine readers).
func Init(verbose, jsonOut bool) {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.DurationFieldUnit = time.Millisecond
	zerolog.DurationFieldInteger = true

	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	if env := strings.ToLower(os.Getenv("SRCMAP_LOG")); env != "" {
		if lvl, err := zerolog.ParseLevel(env); err == nil {
			level = lvl
		}
	}
	zerolog.SetGlobalLevel(level)

	var out io.Writer = os.Stderr
	if !jsonOut {
		out = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05.000",
			NoColor:    os.Getenv("NO_COLOR") != "",
		}
	}
	log.Logger = zerolog.New(out).With().Timestamp().Logger()
}

// Stage begins a timed stage log. Call Done on success or Fail on error.
// Both pair a "start" Info log with either "done" (Info) or "failed" (Error)
// and attach elapsed time. Extra kv pairs apply to the terminal event.
func Stage(name string, kv ...any) *Timer {
	t := &Timer{name: name, start: time.Now()}
	ev := log.Info().Str("stage", name)
	applyKV(ev, kv)
	ev.Msg("start")
	return t
}

// Timer is the handle returned by Stage.
type Timer struct {
	name  string
	start time.Time
}

// Done logs a successful completion with elapsed time.
func (t *Timer) Done(kv ...any) {
	ev := log.Info().Str("stage", t.name).Dur("took", time.Since(t.start))
	applyKV(ev, kv)
	ev.Msg("done")
}

// Fail logs a failed completion with elapsed time and the error.
func (t *Timer) Fail(err error, kv ...any) {
	ev := log.Error().Err(err).Str("stage", t.name).Dur("took", time.Since(t.start))
	applyKV(ev, kv)
	ev.Msg("failed")
}

// Warn logs a non-fatal failure (e.g. a retryable attempt) with elapsed time.
func (t *Timer) Warn(err error, msg string, kv ...any) {
	ev := log.Warn().Err(err).Str("stage", t.name).Dur("took", time.Since(t.start))
	applyKV(ev, kv)
	ev.Msg(msg)
}

func applyKV(ev *zerolog.Event, kv []any) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		ev.Interface(k, kv[i+1])
	}
}
