/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package dhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chuck1024/gd/dlog"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"reflect"
)

var ptrToGinCtx = reflect.PtrTo(reflect.TypeOf((*gin.Context)(nil))).Kind()
var errInterface = reflect.TypeOf((*error)(nil)).Elem()

func CheckWrap(toWrap interface{}) error {
	wt := reflect.TypeOf(toWrap)
	if wt.Kind() != reflect.Func {
		return fmt.Errorf("toWrap must be func,type=%v,func=%v", wt, toWrap)
	}
	wtNumIn := wt.NumIn()
	if wtNumIn < 2 {
		return fmt.Errorf("params in count must > 2 %v", toWrap)
	}
	if wt.In(0).Kind() != ptrToGinCtx {
		return fmt.Errorf("first param in must *gin.Context %v", toWrap)
	}
	if wt.NumOut() < 4 {
		return fmt.Errorf("params out count must > 4 %v", toWrap)
	}
	if wt.Out(0).Kind() != reflect.Int {
		return fmt.Errorf("params out 1 must be int %v", toWrap)
	}
	if wt.Out(1).Kind() != reflect.String {
		return fmt.Errorf("params out 2 must be string %v", toWrap)
	}
	if !wt.Out(2).Implements(errInterface) {
		return fmt.Errorf("params out 3 must be error %v", toWrap)
	}
	return nil
}

// example: wrap to gin.HandlerFunc -- func(*Context)
func Wrap(toWrap interface{}) gin.HandlerFunc {
	refToWrap := reflect.ValueOf(toWrap)
	wt := reflect.TypeOf(toWrap)
	if wt.Kind() != reflect.Func {
		dlog.Crashf("the handler is not func:%v", toWrap)
	}
	wtNumIn := wt.NumIn()
	inType := wt.In(1)
	inParamsTypeList := make([]reflect.Kind, wtNumIn)
	inParamsValueList := make([]reflect.Value, wtNumIn)
	for i := 0; i < wtNumIn; i++ {
		inParamsTypeList[i] = wt.In(i).Kind()
		inParamsValueList[i] = reflect.New(wt.In(i))
		if wt.In(i).Kind() == reflect.Ptr {
			inParamsValueList[i] = reflect.New(wt.In(i).Elem())
		}
	}

	wrapped := func(c *gin.Context) {
		var inVal reflect.Value
		if inType.Kind() == reflect.Ptr {
			ite := inType.Elem()
			inVal = reflect.New(ite)
		} else {
			inVal = reflect.New(inType).Elem()
		}
		inValInterface := inVal.Interface()

		// parse data
		// data_raw is possible to encrypt data
		dataBtsObj, ok := c.Get(DataRaw)
		if !ok {
			if c.Request.Method == http.MethodGet {
				// TODO fix
				c.Bind(inValInterface)
				c.Set(Data, inValInterface)
				dlog.WarnT("http %s shouldn't include body", http.MethodGet)
			} else {
				err := c.Bind(inValInterface)
				if err != nil {
					var body []byte
					var readBodyErr error
					if c.Request.Method == http.MethodPost {
						body, readBodyErr = ioutil.ReadAll(c.Request.Body)
					} else {
						body = []byte(c.Request.RequestURI)
					}
					dlog.Error("wrap data not valid!data=%s,func=%v,err=%v,readBodyErr=%v", string(body), toWrap, err, readBodyErr)
					Return(c, http.StatusBadRequest, "data not valid", err, nil)
					c.Set(SessionLogLevel, "INFO")
					return
				}
				c.Set(Data, inValInterface)
			}
		} else {
			dataBts, ok := dataBtsObj.([]byte)
			if !ok {
				dlog.Error("wrap data not []byte!func=%v,data=%v", toWrap, dataBtsObj)
				Return(c, http.StatusInternalServerError, "data not byte array", nil, nil)
				c.Set(SessionLogLevel, "INFO")
				return
			}
			if dataBts != nil && len(dataBts) > 0 {
				jsonErr := json.Unmarshal(dataBts, inValInterface)
				if jsonErr != nil {
					dlog.Info("wrap wrap data from json fail!bts=%s,func=%v,err=%v", string(dataBts), toWrap, jsonErr)
					Return(c, http.StatusInternalServerError, "data type not valid", jsonErr, nil)
					c.Set(SessionLogLevel, "INFO")
					return
				}
			} else {
				if inType.Kind() == reflect.Ptr {
					inVal = reflect.Zero(inType)
					inValInterface = inVal.Interface()
				}
			}
			c.Set(Data, inValInterface)
		}

		//in := make([]reflect.Value, wtNumIn)
		//in[0] = reflect.ValueOf(c)
		//in[1] = inVal
		out := refToWrap.Call(inParamsValueList)
		if len(out) != 4 {
			//dlog.Error("wrap return not 4!in=%v,out=%v,func=%v", in, out, toWrap)
			dlog.Error("wrap return not 4!out=%v,func=%v", out, toWrap)
			Return(c, http.StatusInternalServerError, "ret not 4!", nil, nil)
			return
		}

		var (
			code    int
			message string
			err     error
			ret     interface{}
		)

		for _, value := range out {
		AGAIN:
			switch value.Kind() {
			case reflect.Int:
				code = int(value.Int())
			case reflect.String:
				message = value.String()
			case reflect.Interface:
				if value.Kind() == reflect.Ptr {
					value = value.Elem()
					goto AGAIN
				}
				if value.CanInterface() {
					if v, ok := value.Interface().(error); ok {
						err = v
					} else {
						ret = value.Interface()
					}
				}
			}
		}

		if code != 0 {
			//dlog.Debug("wrap wrapped call,in=%v,out=%v,func=%v", in, out, toWrap)
			dlog.Debug("wrap wrapped call,out=%v,func=%v", out, toWrap)
			Return(c, code, message, err, ret)
		}

		//if out[0].CanInterface() {
		//	code, _ = out[0].Interface().(int)
		//} else {
		//	code = http.StatusInternalServerError
		//	dlog.Error("wrap not parse code!in=%v,out=%v,func=%v", in, out, toWrap)
		//}
		//
		//if out[1].CanInterface() {
		//	message, _ = out[1].Interface().(string)
		//} else {
		//	dlog.Error("wrap not parse message!in=%v,out=%v,func=%v", in, out, toWrap)
		//}
		//
		//if out[2].CanInterface() {
		//	err, _ = out[2].Interface().(error)
		//} else {
		//	dlog.Error("wrap not parse err!in=%v,out=%v,func=%v", in, out, toWrap)
		//}
		//if out[3].CanInterface() {
		//	ret = out[3].Interface()
		//} else {
		//	dlog.Error("wrap not parse result!in=%v,out=%v,func=%v", in, out, toWrap)
		//}
		//
		//dlog.Debug("wrap wrapped call,in=%v,out=%v,func=%v", in, out, toWrap)
		//Return(c, code, message, err, ret)
	}
	return wrapped
}

func Return(c *gin.Context, code int, message string, err error, result interface{}) {
	ret := make(map[string]interface{})
	ret["code"] = code
	ret["result"] = result
	ret["message"] = message

	c.Set(Ret, ret)
	c.Set(Code, code)
	if err != nil {
		c.Set(Err, err)
	}
}

func ParseRet(c *gin.Context) (ret interface{}, origErr interface{}) {
	origErr, _ = c.Get(Err)
	retObj, ok := c.Get(Ret)
	if !ok {
		if origErr == nil {
			err := errors.New("no ret found")
			c.Set(Err, err)
		}
		ret = gin.H{
			"code":    http.StatusInternalServerError,
			"result":  nil,
			"message": "no result",
		}
	} else {
		if retObj == nil {
			if origErr != nil {
				err := fmt.Errorf("ret empty?,origRet=%v,origErr=%v", retObj, origErr)
				c.Set(Err, err)
			} else {
				err := fmt.Errorf("ret empty?,origRet=%v", retObj)
				c.Set(Err, err)
			}

			ret = gin.H{
				"code":    http.StatusInternalServerError,
				"result":  nil,
				"message": "empty result",
			}
		} else {
			ret = retObj
			return
		}
	}
	return
}
