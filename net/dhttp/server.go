/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package dhttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/chuck1024/gd/dlog"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

type HttpServerInit func(g *gin.Engine) error

type HttpServer struct {
	server *http.Server
	g      *gin.Engine

	GinLog                    bool           `inject:"httpServerGinLog" canNil:"true"`
	UseHttps                  bool           `inject:"httpServerUseHttps" canNil:"true"`
	HttpsCertFile             string         `inject:"httpServerHttpsCertFile" canNil:"true"`
	HttpsKeyFile              string         `inject:"httpServerHttpsKeyFile" canNil:"true"`
	HttpServerShutdownTimeout int64          `inject:"httpServerShutdownTimeout" canNil:"true"`
	HttpServerReadTimeout     int64          `inject:"httpServerReadTimeout" canNil:"true"`
	HttpServerWriteTimeout    int64          `inject:"httpServerWriteTimeout" canNil:"true"`
	HttpServerRunAddr         string         `inject:"httpServerRunAddr" canNil:"true"`
	HttpServerRunPort         int            `inject:"httpServerRunPort"`
	HttpServerInit            HttpServerInit `inject:"httpServerInit"`

	HandlerMap map[string]interface{}
}

func (h *HttpServer) Start() error {
	defer func() {
		dlog.Info("http server start http server with:shutdownTimeout=%d,readTimeout=%d,writeTimeout=%d", h.HttpServerShutdownTimeout, h.HttpServerReadTimeout, h.HttpServerWriteTimeout)
	}()

	if h.UseHttps {
		if h.HttpsCertFile == "" || h.HttpsKeyFile == "" {
			return errors.New("https cert file or key file not set")
		}
	}

	if h.HttpServerReadTimeout <= 0 {
		h.HttpServerReadTimeout = 10
	}

	if h.HttpServerWriteTimeout <= 0 {
		h.HttpServerWriteTimeout = 10
	}

	if h.HttpServerShutdownTimeout <= 0 {
		h.HttpServerShutdownTimeout = 20
	}

	err := h.makeHttpServer()
	if err != nil {
		return err
	}

	go func() {
		var err error
		if h.UseHttps {
			err = h.server.ListenAndServeTLS(h.HttpsCertFile, h.HttpsKeyFile)
		} else {
			err = h.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			msg := fmt.Sprintf("graceful start http server fail,%v", err)
			dlog.Crash(msg)
		}
	}()

	return nil
}

func (h *HttpServer) Close() {
	if h.server == nil {
		dlog.Info("not graceful http server shutdown %d", h.HttpServerRunPort)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.HttpServerShutdownTimeout)*time.Second)
	defer cancel()
	if err := h.server.Shutdown(ctx); err != nil {
		dlog.Error("http server shutdown fail,host=%s,timeout=%d,err=%v", h.HttpServerRunPort, h.HttpServerShutdownTimeout, err)
	} else {
		dlog.Info("http server shutdown %d", h.HttpServerRunPort)
	}
}

func (h *HttpServer) addHandler(url string, handle interface{}) {
	if h.HandlerMap == nil {
		h.HandlerMap = make(map[string]interface{})
	}
	h.HandlerMap[url] = handle
}

func (h *HttpServer) CheckHandle() error {
	for _, v := range h.HandlerMap {
		if err := CheckWrap(v); err != nil {
			return err
		}
	}
	return nil
}

func (h *HttpServer) makeHttpServer() error {
	err := h.initGin()
	if err != nil {
		return err
	}

	s := &http.Server{
		Addr:         fmt.Sprintf(":%d", h.HttpServerRunPort),
		Handler:      h.g,
		ReadTimeout:  time.Duration(h.HttpServerReadTimeout) * time.Second,
		WriteTimeout: time.Duration(h.HttpServerWriteTimeout) * time.Second,
	}

	if len(h.HttpServerRunAddr) > 0 {
		s.Addr = fmt.Sprintf("%s:%d", h.HttpServerRunAddr, h.HttpServerRunPort)
	}

	h.server = s
	return nil
}

func (h *HttpServer) initGin() error {
	var g *gin.Engine
	gin.SetMode(gin.ReleaseMode)
	if !h.GinLog {
		g = gin.New()
		g.Use(gin.Recovery())
	} else {
		g = gin.Default()
	}

	err := h.HttpServerInit(g)
	if err != nil {
		return err
	}

	h.g = g
	return nil
}

// For GET, POST, PUT, PATCH and DELETE requests the respective shortcut
// functions can be used.
func (h *HttpServer) Handle(group *gin.RouterGroup, httpMethod, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.Handle(httpMethod, relativePath, ginHandler)
}

func (h *HttpServer) POST(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.POST(relativePath, ginHandler)
}

func (h *HttpServer) GET(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.GET(relativePath, ginHandler)
}

func (h *HttpServer) DELETE(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.DELETE(relativePath, ginHandler)
}

func (h *HttpServer) PATCH(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.PATCH(relativePath, ginHandler)
}

func (h *HttpServer) PUT(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.PUT(relativePath, ginHandler)
}

func (h *HttpServer) OPTIONS(group *gin.RouterGroup, relativePath string, handler interface{}) {
	h.addHandler(relativePath, handler)
	ginHandler := Wrap(handler)
	group.OPTIONS(relativePath, ginHandler)
}

func (h *HttpServer) Static(group *gin.RouterGroup, relativePath string, path string) {
	group.Static(relativePath, path)
}

func (h *HttpServer) StaticFS(group *gin.RouterGroup, relativePath string, fs http.FileSystem) {
	group.StaticFS(relativePath, fs)
}

func (h *HttpServer) StaticFile(group *gin.RouterGroup, relativePath, filepath string) {
	group.StaticFile(relativePath, filepath)
}

func (h *HttpServer) StaticFileFS(group *gin.RouterGroup, relativePath, filepath string, fs http.FileSystem) {
	group.StaticFileFS(relativePath, filepath, fs)
}
