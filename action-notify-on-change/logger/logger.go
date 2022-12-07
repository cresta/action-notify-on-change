package logger

import (
	"bytes"
	"testing"

	"go.uber.org/fx/fxevent"

	"github.com/sethvargo/go-githubactions"
)

type Logger interface {
	Infof(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Errorf(s string, args ...interface{})
}

type ghLogger struct {
	action *githubactions.Action
}

func (g *ghLogger) Errorf(s string, args ...interface{}) {
	g.action.Errorf(s, args...)
}

func (g *ghLogger) Debugf(format string, args ...interface{}) {
	g.action.Debugf(format, args...)
}

func (g *ghLogger) Infof(format string, args ...interface{}) {
	g.action.Infof(format, args...)
}

var _ Logger = (*ghLogger)(nil)

func NewGhLogger(action *githubactions.Action) Logger {
	return &ghLogger{
		action: action,
	}
}

type TestLogger struct {
	t *testing.T
}

func (t *TestLogger) Errorf(format string, args ...interface{}) {
	t.t.Helper()
	t.t.Logf("[error] "+format, args...)
}

func (t *TestLogger) Debugf(format string, args ...interface{}) {
	t.t.Helper()
	t.t.Logf("[debug] "+format, args...)
}

func (t *TestLogger) Infof(format string, args ...interface{}) {
	t.t.Helper()
	t.t.Logf("[info] "+format, args...)
}

func NewTestLogger(t *testing.T) Logger {
	return &TestLogger{
		t: t,
	}
}

type FxLogger struct {
	logger Logger
}

func (f *FxLogger) LogEvent(event fxevent.Event) {
	var buf bytes.Buffer
	cl := fxevent.ConsoleLogger{W: &buf}
	cl.LogEvent(event)
	switch e := event.(type) {
	case *fxevent.Started:
		if e.Err != nil {
			f.logger.Errorf("Failed to start: %v", e.Err)
		} else {
			f.logger.Infof("Started")
		}
	case *fxevent.Invoked:
		if e.Err != nil {
			f.logger.Errorf("Failed to invoke: %v", e.Err)
		} else {
			f.logger.Infof("Invoked")
		}
	default:
		if buf.Len() > 0 {
			f.logger.Debugf(buf.String())
		}
	}
}

func NewFxLogger(logger Logger) fxevent.Logger {
	return &FxLogger{
		logger: logger,
	}
}
