package log

import (
	"fmt"

	klog "k8s.io/klog/v2"

	kindLog "sigs.k8s.io/kind/pkg/log"
)

type (
	Adapter  struct{ klog.Logger }
	AdapterV struct{ klog.Logger }
)

var (
	_ kindLog.InfoLogger = &AdapterV{}
	_ kindLog.Logger     = &Adapter{}
)

func (l *Adapter) Warn(message string)                      { l.Logger.V(0).Info(message) }
func (l *Adapter) Warnf(format string, args ...any)         { infof(l.Logger.V(0).Info, format, args...) }
func (l *Adapter) Error(message string)                     { l.Logger.V(0).Info(message) }
func (l *Adapter) Errorf(format string, args ...any)        { infof(l.Logger.V(0).Info, format, args...) }
func (l *Adapter) V(level kindLog.Level) kindLog.InfoLogger { return &AdapterV{l.Logger.V(int(level))} }
func (l *AdapterV) Enabled() bool                           { return l.Logger.Enabled() }
func (l *AdapterV) Info(message string)                     { l.Logger.Info(message) }
func (l *AdapterV) Infof(format string, args ...any)        { infof(l.Logger.Info, format, args...) }

func infof(fn func(string, ...any), format string, args ...any) { fn(fmt.Sprintf(format, args...)) }
