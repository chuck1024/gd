/**
 * Copyright 2020 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package gd

import (
	"fmt"
	"github.com/chuck1024/gd/dlog"
	"os"
	"path/filepath"
	"strings"
)

var (
	defaultLogLevel = "DEBUG"
	defaultFormat   = "%L	%D %T	%l	%I	%G	%M	%S"
)

type gdConfig struct {
	BinName    string `json:"binName"`
	Port       int    `json:"port"`
	LogLevel   string `json:"logLevel"`
	LogDir     string `json:"logDir"`
	Stdout     string `json:"stdout"`
	Format     string `json:"format"`
	Rotate     string `json:"rotate"`
	Maxsize    string `json:"maxsize"`
	MaxLines   string `json:"maxLines"`
	RotateType string `json:"rotateType"` // daily hourly
}

func getInfoFileName(binName string, port int) string {
	if port == 0 {
		return fmt.Sprintf("%s.log", binName)
	}
	return fmt.Sprintf("%s_%d.log", binName, port)
}

func getWarnFileName(binName string, port int) string {
	if port == 0 {
		return fmt.Sprintf("%s_err.log", binName)
	}
	return fmt.Sprintf("%s_err_%d.log", binName, port)
}

func (g *gdConfig) initLogConfig() error {
	if g.BinName == "" {
		ex, err := os.Executable()
		if err != nil {
			return err
		}
		exPath := filepath.Dir(ex)
		if strings.Contains(exPath, "/") {
			ex = ex[len(exPath)+1:]
		}
		g.BinName = ex
	}

	if g.LogLevel != "DEBUG" && g.LogLevel != "INFO" && g.LogLevel != "WARNING" && g.LogLevel != "ERROR" {
		return fmt.Errorf("invalid log level %v", g.LogLevel)
	}

	infoFileName := getInfoFileName(g.BinName, g.Port)
	warnFileName := getWarnFileName(g.BinName, g.Port)

	var filters []dlog.XmlFilter
	// stdout
	stdout := dlog.XmlFilter{
		Enabled: g.Stdout,
		Tag:     "stdout",
		Level:   g.LogLevel,
		Type:    "console",
		Property: []dlog.XmlProperty{
			dlog.XmlProperty{Name: "format", Value: g.Format},
		},
	}
	filters = append(filters, stdout)

	toFile := "false"
	if len(g.LogDir) > 0 {
		toFile = "true"
	}
	// info
	info := dlog.XmlFilter{
		Enabled: toFile,
		Tag:     "service",
		Level:   g.LogLevel,
		Type:    "file",
		Property: []dlog.XmlProperty{
			dlog.XmlProperty{Name: "filename", Value: fmt.Sprintf("%s/%s", g.LogDir, infoFileName)},
			dlog.XmlProperty{Name: "format", Value: g.Format},
			dlog.XmlProperty{Name: "rotate", Value: g.Rotate},
			dlog.XmlProperty{Name: "maxsize", Value: g.Maxsize},
			dlog.XmlProperty{Name: "maxLines", Value: g.MaxLines},
			dlog.XmlProperty{Name: g.RotateType, Value: "true"},
		},
	}
	filters = append(filters, info)
	// warn
	warn := dlog.XmlFilter{
		Enabled: toFile,
		Tag:     "service_err",
		Level:   "WARNING",
		Type:    "file",
		Property: []dlog.XmlProperty{
			dlog.XmlProperty{Name: "filename", Value: fmt.Sprintf("%s/%s", g.LogDir, warnFileName)},
			dlog.XmlProperty{Name: "format", Value: g.Format},
			dlog.XmlProperty{Name: "rotate", Value: g.Rotate},
			dlog.XmlProperty{Name: "maxsize", Value: g.Maxsize},
			dlog.XmlProperty{Name: "maxLines", Value: g.MaxLines},
			dlog.XmlProperty{Name: g.RotateType, Value: "true"},
		},
	}
	filters = append(filters, warn)

	c := &dlog.XmlLoggerConfig{
		Filter: filters,
	}

	dlog.LoadConfigurationByXml(c)
	return nil
}

func LogClose() {
	dlog.Close()
}

func Debug(arg0 interface{}, args ...interface{}) {
	dlog.Debug(arg0, args...)
}

func Crash(args ...interface{}) {
	dlog.Crash(args...)
}

func Crashf(format string, args ...interface{}) {
	dlog.Crashf(format, args...)
}

func Exit(args ...interface{}) {
	dlog.Exit(args...)
}

func Exitf(format string, args ...interface{}) {
	dlog.Exitf(format, args...)
}

func Stderr(args ...interface{}) {
	dlog.Stderr(args...)
}

func Stderrf(format string, args ...interface{}) {
	dlog.Stderrf(format, args...)
}

func Stdout(args ...interface{}) {
	dlog.Stdout(args...)
}

func Stdoutf(format string, args ...interface{}) {
	dlog.Stdoutf(format, args...)
}

func GetLevel() string {
	return dlog.GetLevel()
}

func SetLevel(lvl int) {
	dlog.SetLevel(lvl)
}

func Log(lvl dlog.Level, source, message string) {
	dlog.Log(lvl, source, message)
}

func Logf(lvl dlog.Level, format string, args ...interface{}) {
	dlog.Logf(lvl, format, args...)
}

func Logc(lvl dlog.Level, closure func() string) {
	dlog.Logc(lvl, closure)
}

func Finest(arg0 interface{}, args ...interface{}) {
	dlog.Finest(arg0, args...)
}

func Fine(arg0 interface{}, args ...interface{}) {
	dlog.Fine(arg0, args...)
}

func DebugT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.DebugT(tag, arg0, args...)
}

func Trace(arg0 interface{}, args ...interface{}) {
	dlog.Trace(arg0, args...)
}

func TraceT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.TraceT(tag, arg0, args...)
}

func Info(arg0 interface{}, args ...interface{}) {
	dlog.Info(arg0, args...)
}

func InfoT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.InfoT(tag, arg0, args...)
}

func Warn(arg0 interface{}, args ...interface{}) {
	dlog.Warn(arg0, args...)
}

func WarnT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.WarnT(tag, arg0, args...)
}

func Error(arg0 interface{}, args ...interface{}) {
	dlog.Error(arg0, args...)
}

func ErrorT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.ErrorT(tag, arg0, args...)
}

func Critical(arg0 interface{}, args ...interface{}) {
	dlog.Critical(arg0, args...)
}

func CriticalT(tag string, arg0 interface{}, args ...interface{}) {
	dlog.CriticalT(tag, arg0, args...)
}
