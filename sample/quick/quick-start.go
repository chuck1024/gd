/**
 * Copyright 2020 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package main

import (
	"github.com/chuck1024/gd"
	"github.com/chuck1024/gd/net/dhttp"
	"github.com/chuck1024/gd/runtime/inject"
	"github.com/gin-gonic/gin"
	"net/http"
)

func HandlerHttp(c *gin.Context, req interface{}) (code int, message string, err error, ret string) {
	gd.Debug("httpServerTest req:%v", req)
	ret = "ok!!!"
	return http.StatusOK, "ok", nil, ret
}

func main() {
	d := gd.Default()
	inject.RegisterOrFail("httpServerInit", func(g *gin.Engine) error {
		r := g.Group("")
		r.Use(
			dhttp.GlFilter(),
			dhttp.GroupFilter(),
			dhttp.Logger("quick-start"),
		)

		d.HttpServer.GET(r, "test", HandlerHttp)
		d.HttpServer.Static(r, "/f/:name", "/var/www")

		if err := d.HttpServer.CheckHandle(); err != nil {
			return err
		}
		return nil
	})

	gd.SetConfig("Server", "httpPort", "10240")

	if err := d.Run(); err != nil {
		gd.Error("Error occurs, error = %s", err.Error())
		return
	}
}
