/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package redisdb

import (
	"errors"
	"fmt"
	log "github.com/chuck1024/gd/dlog"
	"github.com/chuck1024/gd/runtime/gl"
	"github.com/chuck1024/gd/runtime/gr"
	"github.com/chuck1024/gd/runtime/pc"
	"github.com/chuck1024/gd/utls"
	"github.com/garyburd/redigo/redis"
	"gopkg.in/ini.v1"
	"math/rand"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxActive    = 500
	DefaultMaxIdle      = 8
	DefaultIdleTimeout  = 300
	DefaultRetryTimes   = 3
	DefaultConnTimeout  = 400
	DefaultReadTimeout  = 700
	DefaultWriteTimeout = 500

	RedisPoolCommonCostMax   = 20
	RedisPoolCmdNormal       = "redis_pool_cmd_normal"
	RedisPoolCmd             = "redis_pool_cmd_%v"
	RedisPoolCmdSlowCount    = "redis_pool_%v_slow_count"
	RedisPoolNormalSlowCount = "redis_pool_common_slow_count"

	glRedisPoolCall     = "redisPool_call"
	glRedisPoolCost     = "redisPool_cost"
	glRedisPoolCallFail = "redisPool_call_fail"

	defaultConf = "conf/conf.ini"
)

type RedisConfig struct {
	Addrs          []string
	MaxActive      int
	MaxIdle        int
	Retry          int
	IdleTimeoutSec int //空闲连接可被回收的判断阈值, 单位: 秒
	ConnTimeoutMs  int64
	ReadTimeoutMs  int64
	WriteTimeoutMs int64
	Password       string
	DbNumber       int
}

type RedisPool struct {
	servers []string
	p       map[string]*redis.Pool
	retry   int
}

type RedisPoolClient struct {
	RedisConfig   *RedisConfig `inject:"redisConfig" canNil:"true"`
	RedisConf     *ini.File    `inject:"redisConf" canNil:"true"`
	RedisConfPath string       `inject:"redisConfPath" canNil:"true"`
	PoolName      string       `inject:"poolName" canNil:"true"`

	redisPool *RedisPool
	startOnce sync.Once
	closeOnce sync.Once
}

func (p *RedisPoolClient) Start() error {
	var err error
	p.startOnce.Do(func() {
		if p.RedisConfig != nil {
			err = p.newRedisPools(p.RedisConfig)
		} else if p.RedisConf != nil {
			err = p.initRedis(p.RedisConf, p.PoolName)
		} else {
			if p.RedisConfPath == "" {
				p.RedisConfPath = defaultConf
			}

			err = p.initObjForRedisDb(p.RedisConfPath)
		}
	})
	return err
}

func (p *RedisPoolClient) Close() {
	p.closeOnce.Do(func() {
		var e error
		for k, v := range p.redisPool.p {
			if v != nil {
				err := v.Close()
				if err != nil {
					if e == nil {
						e = err
					}
					log.Error("redis pool close fail,servers=%v,err=%v,k=%v", p.redisPool.servers, err, k)
				}
			}
		}
		if e == nil {
			log.Info("redis pool close ok,servers=%v", p.redisPool.servers)
		}
	})
}

func (p *RedisPoolClient) newRedisPools(cfg *RedisConfig) error {
	if len(cfg.Addrs) <= 0 {
		return errors.New("servers empty")
	}
	maxActive := cfg.MaxActive
	if maxActive <= 0 {
		maxActive = DefaultMaxActive
	}
	maxIdle := cfg.MaxIdle
	if maxIdle <= 0 {
		maxIdle = DefaultMaxIdle
	}
	idleTimeout := cfg.IdleTimeoutSec
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	retry := cfg.Retry
	if retry <= 0 {
		retry = DefaultRetryTimes
	}
	connTimeout := time.Duration(cfg.ConnTimeoutMs) * time.Millisecond
	if connTimeout <= 0 {
		connTimeout = DefaultConnTimeout * time.Millisecond
	}
	readTimeout := time.Duration(cfg.ReadTimeoutMs) * time.Millisecond
	if readTimeout <= 0 {
		readTimeout = DefaultReadTimeout * time.Millisecond
	}
	writeTimeout := time.Duration(cfg.WriteTimeoutMs) * time.Millisecond
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout * time.Millisecond
	}

	cfg4Log := *cfg
	cfg4Log.MaxActive = maxActive
	cfg4Log.MaxIdle = maxIdle
	cfg4Log.IdleTimeoutSec = idleTimeout
	cfg4Log.Retry = retry
	cfg4Log.ConnTimeoutMs = int64(connTimeout / time.Millisecond)
	cfg4Log.ReadTimeoutMs = int64(readTimeout / time.Millisecond)
	cfg4Log.WriteTimeoutMs = int64(writeTimeout / time.Millisecond)

	p.redisPool = newPoolRetryTimeout(cfg.Addrs, cfg.Password, cfg.DbNumber, maxActive, maxIdle, idleTimeout, retry, connTimeout, readTimeout, writeTimeout)
	log.Info("start redis pool,server=%v,cfg=%v", p.redisPool.servers, &cfg4Log)
	return nil
}

func newPoolRetryTimeout(servers []string, password string, dbNumber, maxActive, maxIdle, idleTimeout int, retry int, connTimeout, readTimeout, writeTimeout time.Duration) *RedisPool {
	pools := make(map[string]*redis.Pool)
	finalServers := make([]string, 0, len(servers))
	for _, tmp := range servers {
		server := tmp
		if server == "" {
			log.Error("invalid codis server:servers=%v", servers)
			continue
		}
		if _, ok := pools[server]; ok {
			log.Warn("repeated server, server=%v", server)
			continue
		}
		finalServers = append(finalServers, server)
		p := &redis.Pool{
			Wait:        false,
			MaxActive:   maxActive,
			MaxIdle:     maxIdle,
			IdleTimeout: time.Duration(idleTimeout) * time.Second,
			Dial: func() (c redis.Conn, err error) {
				c, err = redis.DialTimeout("tcp", server, connTimeout, readTimeout, writeTimeout)
				if err == nil {
					if password != "" {
						if _, err = c.Do("AUTH", password); err != nil {
							log.Error("redis auth fail,server=%s,err=%v", server, err)
							c.Close()
							return nil, err
						}
					}
				} else {
					log.Warn("redis dial tcp fail,server=%s,err=%v", server, err)
					return nil, err
				}

				if _, err = c.Do("SELECT", dbNumber); err != nil {
					log.Error("redis select fail, server=%s,err=%v", server, err)
					c.Close()
					return nil, err
				}
				return
			},
		}
		pools[server] = p
	}

	rp := &RedisPool{
		servers: finalServers,
		p:       pools,
		retry:   retry,
	}
	return rp
}

func (p *RedisPoolClient) initObjForRedisDb(redisConfPath string) error {
	redisConfRealPath := redisConfPath
	if redisConfRealPath == "" {
		return errors.New("redisConf not set in g_cfg")
	}

	if !strings.HasSuffix(redisConfRealPath, ".ini") {
		return errors.New("redisConf not an ini file")
	}

	redisConf, err := ini.Load(redisConfRealPath)
	if err != nil {
		return err
	}

	if err = p.initRedis(redisConf, p.PoolName); err != nil {
		return err
	}
	return nil
}

func (p *RedisPoolClient) initRedis(f *ini.File, pn string) error {
	r := f.Section(fmt.Sprintf("%s.%s", "Redis", pn))
	addr := r.Key("addr").String()
	password := r.Key("password").String()
	maxActive, _ := r.Key("maxActive").Int()
	maxIdle, _ := r.Key("maxIdle").Int()
	retry, _ := r.Key("retry").Int()
	idleTimeout, _ := r.Key("idleTimeout").Int()
	connTimeout, _ := r.Key("connTimeout").Int64()
	readTimeout, _ := r.Key("readTimeout").Int64()
	writeTimeout, _ := r.Key("writeTimeout").Int64()
	dbNumber, _ := r.Key("dbNumber").Int()

	addrs := strings.Split(addr, ",")
	err := p.newRedisPools(&RedisConfig{
		Addrs:          addrs,
		MaxActive:      maxActive,
		MaxIdle:        maxIdle,
		Retry:          retry,
		IdleTimeoutSec: idleTimeout,
		ConnTimeoutMs:  connTimeout,
		ReadTimeoutMs:  readTimeout,
		WriteTimeoutMs: writeTimeout,
		Password:       password,
		DbNumber:       dbNumber,
	})

	if err != nil {
		return err
	}

	return nil
}

func (p *RedisPoolClient) Do(commandName string, args ...interface{}) (reply interface{}, err error) {
	sTime := time.Now()
	defer func() {
		cost := time.Now().Sub(sTime)
		pcKey := fmt.Sprintf(RedisPoolCmd, strings.ToLower(commandName))
		pc.Cost(fmt.Sprintf("reidsPool,name=%v,cmd=%s", p.redisPool.servers, pcKey), cost)

		gl.Incr(glRedisPoolCall, 1)
		gl.IncrCost(glRedisPoolCost, cost)

		if cost/time.Millisecond > RedisPoolCommonCostMax {
			pc.Incr(RedisPoolNormalSlowCount, 1)
			if cost/time.Millisecond > 100 {
				log.Warn("redisPool slow, pool:%v, cmd:%v, key:%v, cost:%v", p.redisPool.servers, commandName, args[0], cost)
			}
			pc.Cost(fmt.Sprintf("reidsPool,name=%s,cmd=%s", p.redisPool.servers, RedisPoolCmdNormal), cost)
		}

		if err != nil && err != redis.ErrNil && err != ErrNil {
			pc.CostFail(fmt.Sprintf("rediPool,name=%v", p.redisPool.servers), 1)
			gl.Incr(glRedisPoolCallFail, 1)
		}
	}()

	turn := 0
	//retry with a new server
	usedIps := make([]string, 0)
	for {
		turn++
		func() {
			conn, server, e := p.getConn(usedIps)
			defer func() {
				if conn != nil {
					ce := conn.Close()
					if ce != nil {
						log.Warn("close conn fail,err=%v", ce)
					}
				}
			}()
			usedIps = append(usedIps, server)
			if e != nil {
				err = e
				return
			}

			reply, err = p.do(conn, turn, commandName, args...)
		}()

		if err == nil {
			return
		} else {
			if turn >= p.redisPool.retry {
				break
			}
		}
	}
	return
}

func (p *RedisPoolClient) ActiveCount() int {
	return p.redisPool.p[p.redisPool.servers[0]].ActiveCount()
}

func (p *RedisPoolClient) do(conn redis.Conn, turn int, commandName string, args ...interface{}) (reply interface{}, err error) {
	reply, err = conn.Do(commandName, args...)
	if err != nil {
		log.Warn("redis do cmd fail,turn=%d,cmd=%s,args=%v,err=%v", turn, commandName, args, err)
	}
	return
}

type BatchReq struct {
	Key  string
	Args interface{}
}

type BatchReply struct {
	Req   *BatchReq
	Reply interface{}
	Err   error
}

func (p *RedisPoolClient) BatchDo(commandName string, req []*BatchReq) (reply []*BatchReply) {
	var back []*BatchReply
	var errReply []*BatchReply
	turn := 0
	for {
		turn++
		back, errReply, req = p.batchDo(turn, commandName, req)
		reply = append(reply, back...)
		if len(req) == 0 {
			return
		} else {
			if turn >= p.redisPool.retry {
				reply = append(reply, errReply...)
				break
			}
		}
	}
	return
}

func (p *RedisPoolClient) batchDo(turn int, commandName string, req []*BatchReq) (reply, errReply []*BatchReply, rest []*BatchReq) {
	c := make(chan BatchReply, len(req))
	poolSize := 5
	if len(req) >= 100 {
		poolSize = 10
	}
	timeout := 500
	fixedPoolTimeout := &gr.FixedGoroutinePoolTimeout{Size: int64(poolSize), Timeout: time.Duration(timeout) * time.Millisecond}
	fixedPoolTimeout.Start()
	for _, v := range req {
		tmp := v
		errTimeout := fixedPoolTimeout.Execute(func() {
			var err error
			var ret interface{}
			if tmp.Args != nil {
				ret, err = p.Do(commandName, redis.Args{tmp.Key}.AddFlat(tmp.Args)...)
			} else {
				ret, err = p.Do(commandName, redis.Args{tmp.Key}...)
			}
			c <- BatchReply{tmp, ret, err}
		})
		if errTimeout != nil {
			c <- BatchReply{tmp, nil, errTimeout}
		}
	}
	fixedPoolTimeout.Close()
	close(c)
	for v := range c {
		tem := v
		if v.Err != nil {
			log.Warn("redis do cmd fail,turn=%d,cmd=%s,args=%v,err=%v", turn, commandName, v.Req.Args, v.Err)
			rest = append(rest, tem.Req)
			errReply = append(errReply, &tem)
		} else {
			reply = append(reply, &tem)
		}
	}
	return
}

func (p *RedisPoolClient) getConn(usedIps []string) (redis.Conn, string, error) {
	perm := rand.Perm(len(p.redisPool.servers))
	for _, idx := range perm {

		server := p.redisPool.servers[idx]
		if utls.StringInSlice(usedIps, server) {
			continue
		}

		conn := p.redisPool.p[server].Get()
		return conn, server, nil
	}

	idx := rand.Int31n(int32(len(p.redisPool.servers)))
	server := p.redisPool.servers[idx]
	conn := p.redisPool.p[server].Get()

	return conn, server, nil
}

/*
*
Redis get
return string if exist

	err = redis.ErrNil if not exist
*/
func (p *RedisPoolClient) Get(key string) (ret string, errRet error) {
	return redis.String(p.Do("GET", key))
}

func (p *RedisPoolClient) Set(key, value string) (err error) {
	_, err = p.Do("SET", key, value)
	return
}

func (p *RedisPoolClient) Del(key string) (err error) {
	_, err = p.Do("DEL", key)
	return
}

func (p *RedisPoolClient) HGetAll(key string) (ret map[string]string, err error) {
	return redis.StringMap(redis.Values(p.Do("HGETALL", key)))
}

func (p *RedisPoolClient) HScan(key string, count int64) (ret map[string]string, err error) {
	res, err := redis.Values(p.Do("HSCAN", key, 0, "COUNT", count))
	if err != nil {
		return nil, err
	}
	if len(res) < 2 {
		return map[string]string{}, nil
	}
	return redis.StringMap(res[1], nil)
}

func (p *RedisPoolClient) HGet(key string, field string) (string, error) {
	return redis.String(p.Do("HGET", key, field))
}

func (p *RedisPoolClient) HDel(key, field string) (err error) {
	_, err = p.Do("HDEL", key, field)
	return
}

func (p *RedisPoolClient) HSet(key string, field string, value string) (err error) {
	_, err = p.Do("HSET", key, field, value)
	return
}

func (p *RedisPoolClient) HMGet(key string, values []string) ([]string, error) {
	return redis.Strings(p.Do("HMGET", redis.Args{key}.AddFlat(values)...))

}

func (p *RedisPoolClient) HMDel(key string, values []string) (int64, error) {
	return redis.Int64(p.Do("HDEL", redis.Args{key}.AddFlat(values)...))

}

func (p *RedisPoolClient) IncrBy(key string, val int64) (int64, error) {
	return redis.Int64(p.Do("INCRBY", key, val))
}

func (p *RedisPoolClient) HIncrBy(key, field string, val int64) (int64, error) {
	return redis.Int64(p.Do("HINCRBY", key, field, val))
}

func (p *RedisPoolClient) Incr(key string) (int, error) {
	return redis.Int(p.Do("INCR", key))
}

func (p *RedisPoolClient) Expire(key string, time int) (int, error) {
	return redis.Int(p.Do("EXPIRE", key, time))
}

func (p *RedisPoolClient) SetNX(key, value string, expire int) (interface{}, error) {
	return p.Do("SET", key, value, "EX", expire, "NX")
}

func (p *RedisPoolClient) MGet(keys []string) (ret []interface{}, errRet error) {
	iArray := make([]interface{}, 0, len(keys))
	for _, v := range keys {
		iArray = append(iArray, v)
	}
	return redis.Values(p.Do("MGET", iArray...))
}

func (p *RedisPoolClient) SetEx(key string, expire int64, value string) (err error) {
	_, err = p.Do("SETEX", key, expire, value)
	return
}

func (p *RedisPoolClient) SAdd(key string, vals []string) error {
	var args []interface{}
	args = append(args, key)
	for _, v := range vals {
		args = append(args, v)
	}
	_, err := p.Do("SADD", args...)
	return err
}

func (p *RedisPoolClient) ZAdd(key string, score int64, val string) error {
	_, err := p.Do("ZADD", key, score, val)
	return err
}

func (p *RedisPoolClient) ZRemByScore(key string, start string, end string) error {
	_, err := p.Do("ZREMRANGEBYSCORE", key, start, end)
	return err
}

func (p *RedisPoolClient) ZRange(key string, start int64, end int64) ([]string, error) {
	return redis.Strings(p.Do("ZRANGE", key, start, end))
}

func (p *RedisPoolClient) Exists(key string) (bool, error) {
	return redis.Bool(p.Do("EXISTS", key))
}

func (p *RedisPoolClient) SPop(key string) (string, error) {
	return redis.String(p.Do("SPOP", key))
}

func (p *RedisPoolClient) LIndex(key string, index int64) (string, error) {
	return redis.String(p.Do("LINDEX", key, index))
}

func (p *RedisPoolClient) LPop(key string) (string, error) {
	return redis.String(p.Do("LPOP", key))
}

func (p *RedisPoolClient) RPush(key, val string) (int64, error) {
	return redis.Int64(p.Do("RPUSH", key, val))
}

func (p *RedisPoolClient) LPush(key string, val string) (int64, error) {
	return redis.Int64(p.Do("LPUSH", key, val))
}

func (p *RedisPoolClient) HLen(key string) (int64, error) {
	return redis.Int64(p.Do("HLEN", key))
}
