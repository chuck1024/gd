/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package mysqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gitee.com/chunanyong/dm"
	"github.com/chuck1024/gd"
	log "github.com/chuck1024/gd/dlog"
	"github.com/chuck1024/gd/runtime/pc"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"gopkg.in/ini.v1"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	defaultDbConf          = "conf/conf.ini"
	dmDataBaseType         = "dm"
	mysqlDataBaseType      = "mysql"
	PcTransactionInsertDup = "transaction_insert_dup"
)

type MysqlClient struct {
	DbConfig   *CommonDbConf `inject:"mysqlDbConfig" canNil:"true"`
	DbConf     *ini.File     `inject:"mysqlDbConf" canNil:"true"`
	DbConfPath string        `inject:"mysqlDbConfPath" canNil:"true"`
	DataBase   string        `inject:"mysqlDatabase" canNil:"true"`

	dbWrite           []*DbWrap
	dbRead            []*DbWrap
	ReadLoadBalancer  DBLoadBalancer
	WriteLoadBalancer DBLoadBalancer

	startOnce sync.Once
	closeOnce sync.Once

	// 数据库类型指定 dm mysql 缺省：mysql
	DbType string

	// 重试次数，默认为 0
	RetryTimes int

	// 检查的结构体标签，默认为 mysqlField
	LookTag string
}

func (c *MysqlClient) Start() error {
	var err error
	c.startOnce.Do(func() {
		if c.DbConfig != nil {
			err = c.initDbsWithCommonConf(c.DbConfig)
		} else if c.DbConf != nil {
			err = c.initDbs(c.DbConf, c.DataBase)
		} else {
			if c.DbConfPath == "" {
				c.DbConfPath = defaultDbConf
			}

			err = c.initObjForMysqlDb(c.DbConfPath)
		}
	})
	return err
}

func (c *MysqlClient) Close() {
	c.closeOnce.Do(func() {
		c.closeMainDbs()
	})
}

func (c *MysqlClient) SetReadLoadBalancer(l DBLoadBalancer) {
	c.ReadLoadBalancer = l
}

func (c *MysqlClient) SetWriteLoadBalancer(l DBLoadBalancer) {
	c.WriteLoadBalancer = l
}

func (c *MysqlClient) getReadDbs() []*DbWrap {
	return c.dbRead
}

func (c *MysqlClient) GetReadDbs() []*DbWrap {
	return c.dbRead
}

func (c *MysqlClient) getReadDb() (*DbWrap, error) {
	return c.ReadLoadBalancer(c.dbRead)
}

func (c *MysqlClient) getWriteDb() (*DbWrap, error) {
	return c.WriteLoadBalancer(c.dbWrite)
}

func (c *MysqlClient) GetWriteDbList() []*DbWrap {
	return c.dbWrite
}

func (c *MysqlClient) GetReadDbList() []*DbWrap {
	return c.dbRead
}

func (c *MysqlClient) GetLBWriteDb() (*DbWrap, error) {
	return c.WriteLoadBalancer(c.dbWrite)
}

func (c *MysqlClient) GetLBReadDb() (*DbWrap, error) {
	return c.ReadLoadBalancer(c.dbRead)
}

func (c *MysqlClient) GetReadDbRandom() (*DbWrap, error) {
	return c.getReadDbRandomly()
}

func (c *MysqlClient) getReadDbRandomly() (*DbWrap, error) {
	dbRead := c.getReadDbs()

	if len(dbRead) <= 0 {
		return nil, fmt.Errorf("no read db found")
	}

	readDb := dbRead[rand.Intn(len(dbRead))]
	return readDb, nil
}

func (c *MysqlClient) getWriteDbs() *DbWrap {
	if len(c.dbWrite) <= 1 {
		return c.dbWrite[0]
	}

	idx := rand.Intn(len(c.dbWrite))
	return c.dbWrite[idx]
}

func (c *MysqlClient) GetWriteDbs() *DbWrap {
	return c.getWriteDbs()
}

func (c *MysqlClient) getWriteDbsArray() []*DbWrap {
	return c.dbWrite
}

func getHostFromConnStr(connStr string, dbType string) (string, error) {
	// dm: dm://SYSDBA:SYSDBA@172.18.35.112:5236?
	// mysql:"%s:%s@tcp(%s:%s)/%s?timeout=%s"
	var fi, ei int
	fi = strings.Index(connStr, "@") + 5
	ei = strings.Index(connStr, "/") - 1
	if dbType == dmDataBaseType {
		ei = strings.LastIndex(connStr, "?")
		fi = strings.Index(connStr, "@") + 1
	}
	if fi <= 0 || ei <= 0 || ei <= fi {
		err := fmt.Errorf("not find host in db conn str:%s,fi=%d,ei=%d", connStr, fi, ei)
		return "", err
	}

	host := connStr[fi:ei]
	if host == "" || host == ":" {
		return "", fmt.Errorf("host not found %s", connStr)
	}

	log.Debug("find host in db conn str:%s for %s", host, connStr)
	return host, nil
}

func (c *MysqlClient) initMainDbsMaxOpen(connMasters, connSlaves []string, maxOpen, maxIdle int, glSuffix, dbType string, timeout time.Duration, masterProxy, slaveProxy bool) error {
	log.Debug("open master=%v,slave=%v", connMasters, connSlaves)
	if len(connMasters) <= 0 {
		return fmt.Errorf("masters empty,master=%v,slave=%v", connMasters, connSlaves)
	}

	c.WriteLoadBalancer = DefaultWriteLoadBalancer
	c.ReadLoadBalancer = DefaultReadLoadBalancer

	if c.LookTag == "" {
		c.LookTag = "mysqlField"
	}

	var dbWrites []*DbWrap
	for _, connMaster := range connMasters {
		db, err := sqlx.Open(dbType, connMaster)
		if err != nil {
			return err
		}
		db.Mapper = reflectx.NewMapper(c.LookTag)
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxIdle)
		hst, err := getHostFromConnStr(connMaster, dbType)
		if err != nil {
			return err
		}
		dbw := NewDbWrappedRetryProxy(hst, db, c, timeout, defaultDbRetry, masterProxy)
		dbw.glSuffix = glSuffix
		dbWrites = append(dbWrites, dbw)
	}
	c.dbWrite = dbWrites

	if connSlaves == nil || len(connSlaves) <= 0 {
		log.Info("read slaves empty, use master for read")
		c.dbRead = c.dbWrite
		return nil
	}

	dbRead := make([]*DbWrap, len(connSlaves))
	for idx, rs := range connSlaves {
		d, err := sqlx.Open(dbType, rs)
		if err != nil {
			return err
		}
		hst, err := getHostFromConnStr(rs, dbType)
		if err != nil {
			return err
		}
		dbr := NewDbWrappedRetryProxy(hst, d, c, timeout, defaultDbRetry, slaveProxy)
		dbr.SetMaxOpenConns(maxOpen)
		dbr.SetMaxIdleConns(maxIdle)
		dbr.glSuffix = glSuffix
		dbRead[idx] = dbr
	}
	c.dbRead = dbRead
	return nil
}

func (c *MysqlClient) closeMainDbs() {
	dbRead := c.dbRead
	dbWrite := c.dbWrite
	for _, dbw := range dbWrite {
		err := dbw.Close()
		if err != nil {
			log.Warn("write close err, %v", err)
		}
	}

	if dbRead != nil {
		for _, r := range dbRead {
			err := r.Close()
			if err != nil {
				log.Warn("read close err, %v", err)
			}
		}
	}
	log.Info("db close finish")
}

// GetCount DM支持该方法 获取表中数据总数 .
func (c *MysqlClient) GetCount(query string, args ...interface{}) (int64, error) {
	total := int64(0)
	if c.DbType == dmDataBaseType {
		query = strings.Replace(query, "`", "", -1)
	}
	row, err := c.queryRow(query, args...)
	if err != nil {
		return 0, err
	}

	err = row.Scan(nil, &total)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	return total, nil
}

func (c *MysqlClient) queryList(query string, args ...interface{}) (*sql.Rows, error) {
	readDbs := c.getReadDbs()
	if readDbs == nil || len(readDbs) == 0 {
		return nil, NoAvailableDBErr
	}

	var randNums []int
	randNums = rand.Perm(len(readDbs))

	var ret *sql.Rows
	var err error
	var retry int
	for _, i := range randNums {
		readDb := readDbs[i]
		log.Debug("MysqlClient queryList, use db:%s", readDb.host)

		ret, err = readDb.Query(query, args...)
		if err == nil {
			return ret, err
		}
		log.Warn("MysqlClient queryList, use db:%s, occur err:%v", readDb.host, err)

		if retry < 1 {
			retry++

			if IsDbConnError(err) {
				continue
			} else {
				errMsg := err.Error()
				if strings.Contains(errMsg, "getsockopt") {
					errT := reflect.TypeOf(err)
					log.Error("SOCKOPT_FAIL", "query err,type=%v,err=%v", errT, err)
					continue
				}
			}
		}

		return ret, err
	}

	log.Warn("no available db...")
	return nil, NoAvailableDBErr
}

func (c *MysqlClient) queryRow(query string, args ...interface{}) (*Row, error) {
	readDbs := c.getReadDbs()
	if readDbs == nil || len(readDbs) == 0 {
		return nil, NoAvailableDBErr
	}

	for i := 0; i < c.RetryTimes+1; i++ {
		readDb, err := c.getReadDb()
		if err != nil {
			log.Debug("get read db failed:%v", err.Error())
			if IsDbConnError(err) {
				continue
			} else {
				errMsg := err.Error()
				if strings.Contains(errMsg, "getsockopt") {
					errT := reflect.TypeOf(err)
					log.Error("SOCKOPT_FAIL", "query err,type=%v,err=%v", errT, err)
					continue
				}
			}
		}
		row := readDb.QueryRow(query, args...)
		if row.err == nil {
			return row, nil
		}
	}

	return nil, NoAvailableDBErr
}

// IsExistTable DM支持该方法 判断表是否存在
func (c *MysqlClient) IsExistTable(tableName string) (bool, error) {
	db := c.DataBase
	if c.DbType == dmDataBaseType {
		ret, err := c.Query((*TableName)(nil), fmt.Sprintf("select SEGMENT_NAME AS TABLE_NAME  from dba_segments where dba_segments.OWNER='%s' and SEGMENT_NAME='%s';", db, tableName))
		if err != nil {
			return false, errors.New(fmt.Sprintf("IsExistTable dm query occur error:%v", err))
		}

		if ret == nil {
			gd.Info("IsExistTable dm Query is nil")
			return false, nil
		}

		return ret.(*TableName).TableName == tableName, nil
	} else {
		ret, err := c.Query((*TableName)(nil), fmt.Sprintf("select TABLE_NAME from INFORMATION_SCHEMA.TABLES where TABLE_SCHEMA = '%s' and  TABLE_NAME ='%s';", db, tableName))
		if err != nil {
			return false, errors.New(fmt.Sprintf("IsExistTable mysql query occur error:%v", err))
		}

		if ret == nil {
			gd.Info("IsExistTable mysql Query is nil")
			return false, nil
		}
		return ret.(*TableName).TableName == tableName, nil
	}
}

// Query DM支持该方法 获取数据，无数据返回nil,nil.
func (c *MysqlClient) Query(dataType interface{}, query string, args ...interface{}) (interface{}, error) {
	fieldNames, err := GetDataStructFields(dataType, c.LookTag)
	if err != nil {
		return nil, err
	}
	t := reflect.TypeOf(dataType)
	var typeOf reflect.Type
	if t.Kind() == reflect.Struct {
		typeOf = reflect.TypeOf(dataType)
	} else if t.Elem().Kind() == reflect.Struct {
		typeOf = reflect.TypeOf(dataType).Elem()
	} else {
		return nil, DataTypeErr
	}
	//typeOf := reflect.TypeOf(dataType).Elem()
	dataObj := reflect.New(typeOf).Interface()
	dests, indexMap, err := GetDataStructDests(dataObj, c.DbType, c.LookTag)
	if err != nil {
		return nil, err
	}

	query = strings.Replace(query, "?", strings.Join(fieldNames, ","), 1)
	if c.DbType == dmDataBaseType {
		query = strings.Replace(query, "`", "", -1)
		keys := strings.Split(strings.ToLower(query), " where ")
		if len(keys) > 1 {
			var builder strings.Builder
			builder.WriteString(" ")
			builder.WriteString("where")
			wks := strings.Split(keys[1], " ")
			for _, key := range wks {
				findTag := false
				for k, _ := range indexMap {
					prefix := strings.ReplaceAll(fieldNames[k], "`", "")
					if strings.HasPrefix(strings.TrimLeft(key, " "), prefix) {
						builder.WriteString(fmt.Sprintf(" text_equal(%s,?)", prefix))
						findTag = true
					}
				}
				if !findTag {
					builder.WriteString(" " + key)
				}
			}
			query = keys[0] + builder.String()
		}
	}

	row, err := c.queryRow(query, args...)
	if err != nil {
		return nil, err
	}
	row.lookTag = c.LookTag
	err = row.Scan(indexMap, dests...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return dataObj, nil
}

// QueryList DM支持该方法 获取数据列表.
func (c *MysqlClient) QueryList(dataType interface{}, query string, args ...interface{}) ([]interface{}, error) {
	fieldNames, err := GetDataStructFields(dataType, c.LookTag)
	if err != nil {
		return nil, err
	}

	query = strings.Replace(query, "?", strings.Join(fieldNames, ","), 1)
	typeOf := reflect.TypeOf(dataType).Elem()
	dataObj := reflect.New(typeOf).Interface()
	_, indexMap, err := GetDataStructDests(dataObj, c.DbType, c.LookTag)
	if err != nil {
		return nil, err
	}
	if c.DbType == dmDataBaseType {
		query = strings.Replace(query, "`", "", -1)
		query = strings.Replace(query, "`", "", -1)
		keys := strings.Split(strings.ToLower(query), " where ")
		if len(keys) > 1 {
			var builder strings.Builder
			builder.WriteString(" ")
			builder.WriteString("where")
			wks := strings.Split(keys[1], " ")
			for _, key := range wks {
				findTag := false
				for k, _ := range indexMap {
					prefix := strings.ReplaceAll(fieldNames[k], "`", "")
					if strings.HasPrefix(strings.TrimLeft(key, " "), prefix) {
						builder.WriteString(fmt.Sprintf(" text_equal(%s,?)", prefix))
						findTag = true
					}
				}
				if !findTag {
					builder.WriteString(" " + key)
				}
			}
			query = keys[0] + builder.String()
		}
	}

	rows, err := c.queryList(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rets []interface{}
	for rows.Next() {
		typeOf := reflect.TypeOf(dataType).Elem()
		dataObj := reflect.New(typeOf).Interface()
		dests, indexMap, err := GetDataStructDests(dataObj, c.DbType, c.LookTag)
		if err != nil {
			return nil, err
		}
		var tempScan []interface{}
		for i, dp := range dests {
			if indexMap == nil {
				tempScan = append(tempScan, dp)
			} else {
				if _, ok := indexMap[i]; ok {
					tempScan = append(tempScan, &dm.DmClob{})
				} else {
					tempScan = append(tempScan, dp)
				}
			}
		}

		if err = rows.Scan(tempScan...); err != nil {
			return nil, err
		}
		// add value from tempDest to dest
		for i, td := range tempScan {
			if dmClob, isok := td.(*dm.DmClob); isok {
				dmlen, errLength := dmClob.GetLength()
				if errLength != nil {
					return nil, errLength
				}

				strInt64 := strconv.FormatInt(dmlen, 10)
				dmlenInt, errAtoi := strconv.Atoi(strInt64)
				if errAtoi != nil {
					return nil, errAtoi
				}

				str, errReadString := dmClob.ReadString(1, dmlenInt)
				if errReadString != nil {
					return nil, errReadString
				}
				dv := reflect.Indirect(reflect.ValueOf(dests[i]))
				if dv.Kind() != reflect.Struct {
					dv.SetString(str)
				} else {
					dv.FieldByName("String").SetString(str)
				}
			}
		}
		rets = append(rets, dataObj)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return rets, nil
}

// Update DM支持该方法 根据主键primaryKeys更新数据.
func (c *MysqlClient) Update(tableName string, d interface{}, primaryKeys map[string]interface{}, fieldsToUpdate []string) error {
	var err error
	fieldNames, err := GetDataStructFields(d, c.LookTag)
	if err != nil {
		return err
	}
	if len(primaryKeys) <= 0 {
		return errors.New("primary keys empty on update")
	}
	escapedName := MysqlEscapeString(tableName)
	tableName = escapedName
	typeOf := reflect.TypeOf(d).Elem()
	dataObj := reflect.New(typeOf).Interface()
	_, indexMap, err := GetDataStructDests(dataObj, c.DbType, c.LookTag)
	if err != nil {
		return err
	}
	// d must be a struct pointer
	typ := reflect.TypeOf(d)
	if typ == nil {
		return fmt.Errorf("input cannot be nil %v", typ)
	}
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("not a struct type %v", typ)
	}

	writeDb := c.getWriteDbs()

	rv := reflect.ValueOf(d)
	te := rv.Elem()
	tt := te.Type()
	nf := te.NumField()
	var fieldNamesArray []string
	var dests []interface{}
	whereFields := ""
	for i := 0; i < nf; i++ {
		tf := tt.Field(i)
		mysqlFieldName := tf.Tag.Get(c.LookTag)
		if mysqlFieldName != "" {
			if len(fieldsToUpdate) > 0 {
				found := false
				for _, f2u := range fieldsToUpdate {
					if f2u == mysqlFieldName {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if c.DbType == dmDataBaseType {
				mysqlFieldName = "" + mysqlFieldName + ""
			} else {
				mysqlFieldName = "`" + mysqlFieldName + "`"
			}
			fieldNamesArray = append(fieldNamesArray, mysqlFieldName+"=?")
		} else {
			continue
		}

		f := te.Field(i)
		if !f.CanInterface() {
			return fmt.Errorf("invalid struct field %s.%s", tt.Name(), tf.Name)
		}
		dests = append(dests, f.Interface())
	}

	fieldsNames := strings.Join(fieldNamesArray, ",")
	lpk := len(primaryKeys)
	pki := 0
	for pkn, pkv := range primaryKeys {
		if pki < lpk-1 {
			if c.DbType == dmDataBaseType {
				findTag := false
				for k, _ := range indexMap {
					prefix := strings.ReplaceAll(fieldNames[k], "`", "")
					if strings.HasPrefix(strings.TrimLeft(pkn, " "), prefix) {
						whereFields = whereFields + fmt.Sprintf(" text_equal(%s,?) AND ", pkn)
						findTag = true
						break
					}
				}
				if !findTag {
					whereFields = whereFields + fmt.Sprintf(" %s = ? AND ", pkn)
				}
			} else {
				whereFields = whereFields + fmt.Sprintf(" `%s` = ? AND ", pkn)
			}
		} else {
			if c.DbType == dmDataBaseType {
				findTag := false
				for k, _ := range indexMap {
					prefix := strings.ReplaceAll(fieldNames[k], "`", "")
					if strings.HasPrefix(strings.TrimLeft(pkn, " "), prefix) {
						whereFields = whereFields + fmt.Sprintf(" text_equal(%s,?)", pkn)
						findTag = true
						break
					}
				}
				if !findTag {
					whereFields = whereFields + fmt.Sprintf(" %s = ?  ", pkn)
				}
			} else {
				whereFields = whereFields + fmt.Sprintf(" `%s` = ? ", pkn)
			}
		}
		dests = append(dests, pkv)
		pki++
	}

	var result sql.Result

	if c.DbType == dmDataBaseType {
		result, err = writeDb.Exec("update "+tableName+" set "+fieldsNames+" where "+whereFields, dests...)
	} else {
		result, err = writeDb.Exec("update `"+tableName+"` set "+fieldsNames+" where "+whereFields, dests...)
	}
	if err != nil {
		return err
	}

	log.Debug("update table=%s,data=%v,ret=%v,err=%v", tableName, d, result, err)
	return nil
}

// Add  DM不支持该方法 推荐使用AddEscapeAutoIncr、AddEscapeAutoIncrAndRetLastId、InsertOrUpdateOnDup.
func (c *MysqlClient) Add(tableName string, d interface{}, ondupUpdate bool) error {
	_, err := c.AddEscapeAutoIncr(tableName, d, ondupUpdate, "")
	return err
}

// AddEscapeAutoIncr DM支持该方法 插入/插入更新 ondupUpdate 插入更新，必须指定自主列名称 atuoincrkey.
func (c *MysqlClient) AddEscapeAutoIncr(tableName string, d interface{}, ondupUpdate bool, atuoincrkey string) (int64, error) {
	result, err := c.addEscapeAutoIncr(tableName, d, ondupUpdate, atuoincrkey)
	if err != nil {
		return -1, err
	}
	return result.LastInsertId()
}

// AddEscapeAutoIncrAndRetLastId DM支持该方法 执行纯插入操作(若数据已存在，则返回失败)，其会跳过由atuoincrkey指定的自增列，若执行成功，返回所插入的行id
func (c *MysqlClient) AddEscapeAutoIncrAndRetLastId(tableName string, d interface{}, atuoincrkey string) (int64, error) {
	sqlRet, err := c.addEscapeAutoIncr(tableName, d, false, atuoincrkey)
	if err != nil {
		return -1, err
	} else {
		return sqlRet.LastInsertId()
	}
}

func (c *MysqlClient) addEscapeAutoIncr(tableName string, d interface{}, ondupUpdate bool, atuoincrkey string) (sql.Result, error) {
	escapedName := MysqlEscapeString(tableName)
	tableName = escapedName

	if atuoincrkey == "" && c.DbType == dmDataBaseType {
		return nil, fmt.Errorf("dm database must have a atuoincrkey")
	}

	// d must be a struct pointer
	typ := reflect.TypeOf(d)
	if typ == nil {
		return nil, fmt.Errorf("input cannot be nil %v", typ)
	}
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("not a struct type %v", typ)
	}

	writeDb := c.getWriteDbs()

	rv := reflect.ValueOf(d)
	te := rv.Elem()
	tt := te.Type()
	nf := te.NumField()
	var fieldNamesArray []string
	var placeHoldersArray []string
	var dests []interface{}
	var selectLang, insertNoAutoIncrLang, updateLang []string
	if c.DbType == dmDataBaseType {
		for i := 0; i < nf; i++ {
			tf := tt.Field(i)
			k := tf.Tag.Get(c.LookTag)
			if k == "" {
				continue
			}
			selectLang = append(selectLang, fmt.Sprintf("? %s", k))
			if atuoincrkey != "" && k == atuoincrkey {
				continue
			}
			insertNoAutoIncrLang = append(insertNoAutoIncrLang, fmt.Sprintf("t2.%s", k))
			updateLang = append(updateLang, " t1."+k+"=t2."+k)
		}
	}

	for i := 0; i < nf; i++ {
		tf := tt.Field(i)
		mysqlFieldName := tf.Tag.Get(c.LookTag)
		if mysqlFieldName != "" {
			if atuoincrkey != "" && mysqlFieldName == atuoincrkey && c.DbType != dmDataBaseType {
				continue
			}
			if c.DbType == dmDataBaseType {
				fieldNamesArray = append(fieldNamesArray, mysqlFieldName)
			} else {
				fieldNamesArray = append(fieldNamesArray, "`"+mysqlFieldName+"`")
			}
			placeHoldersArray = append(placeHoldersArray, "?")
		} else {
			continue
		}

		f := te.Field(i)
		if !f.CanInterface() {
			return nil, fmt.Errorf("invalid struct field %s.%s", tt.Name(), tf.Name)
		}
		dests = append(dests, f.Interface())
	}
	fieldsNames := strings.Join(fieldNamesArray, ",")
	placeHolders := strings.Join(placeHoldersArray, ",")

	sqlStr := "insert into `" + tableName + "`(" + fieldsNames + ") values (" + placeHolders + ")"
	if ondupUpdate {
		// 组装dm的插入更新  merge into
		if c.DbType == dmDataBaseType {
			sqlStr = "merge into " + tableName + " as t1" +
				" using (select " + strings.Join(selectLang, ",") +
				" from dual ) as t2 on t1." + atuoincrkey + " = t2." + atuoincrkey +
				" when matched then update set " + strings.Join(updateLang, ",") +
				" when not matched then insert (" + strings.ReplaceAll(strings.Join(insertNoAutoIncrLang, ","), "t2.", "") + ") values  (" + strings.Join(insertNoAutoIncrLang, ",") + ")"
		} else {
			setStr := ""
			fnl := len(fieldNamesArray)
			for i, fn := range fieldNamesArray {
				if i < fnl-1 {
					setStr += fmt.Sprintf("%s = ?,", fn)
				} else {
					setStr += fmt.Sprintf("%s = ?", fn)
				}
			}

			for _, dt := range dests {
				dests = append(dests, dt)
			}
			sqlStr = sqlStr + " ON DUPLICATE KEY UPDATE " + setStr
		}
	}

	if c.DbType == dmDataBaseType {
		var builder strings.Builder
		builder.Grow(4)
		builder.WriteString("set identity_insert " + tableName + " on;")
		builder.WriteString(strings.ReplaceAll(sqlStr+";", "`", ""))
		builder.WriteString("set identity_insert " + tableName + " off;")
		builder.WriteString("commit;")
		sqlStr = builder.String()
	}

	result, err := writeDb.Exec(sqlStr, dests...)
	if err != nil {
		return nil, err
	}

	log.Debug("insert table=%s, data=%v,ret=%v,err=%v", tableName, d, result, err)
	return result, nil
}

// InsertOrUpdateOnDup 根据主键/插入更新 useSqlOnDup=false，根据primaryKeys更新数据。useSqlOnDup=true,主键重复则更新updateFields，否则插入新数据主键ID自增.
func (c *MysqlClient) InsertOrUpdateOnDup(tableName string, d interface{}, primaryKeys []string, updateFields []string, useSqlOnDup bool) (int64, error) {

	if len(primaryKeys) <= 0 || len(updateFields) <= 0 {
		return 0, errors.New("primaryKeys or updateFields are nil")
	}

	escapedName := MysqlEscapeString(tableName)
	tableName = escapedName

	typ := reflect.TypeOf(d)
	if typ == nil {
		return 0, fmt.Errorf("input cannot be nil %v", typ)
	}
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return 0, fmt.Errorf("not a struct type %v", typ)
	}

	rv := reflect.ValueOf(d)
	te := rv.Elem()
	tt := te.Type()
	nf := te.NumField()

	var insertSqlFieldNames []string
	var insertSqlPlaceHolders []string
	var insertSqlFieldValues []interface{}
	var updateSqlFieldNames []string
	var updateSqlFieldValues []interface{}
	var whereSqlFieldNames []string
	var whereSqlFieldValues []interface{}

	for i := 0; i < nf; i++ {
		tf := tt.Field(i)
		mysqlFieldName := tf.Tag.Get(c.LookTag)
		if mysqlFieldName == "" {
			continue
		}

		f := te.Field(i)
		if !f.CanInterface() {
			return 0, fmt.Errorf("invalid struct field %s.%s", tt.Name(), tf.Name)
		}

		insertSqlFieldNames = append(insertSqlFieldNames, "`"+mysqlFieldName+"`")
		insertSqlPlaceHolders = append(insertSqlPlaceHolders, "?")
		insertSqlFieldValues = append(insertSqlFieldValues, f.Interface())

		for _, updateField := range updateFields {
			if updateField == mysqlFieldName {
				updateSqlFieldNames = append(updateSqlFieldNames, "`"+mysqlFieldName+"`=?")
				updateSqlFieldValues = append(updateSqlFieldValues, f.Interface())
				break
			}
		}

		for _, primaryKey := range primaryKeys {
			if primaryKey == mysqlFieldName {
				if c.DbType == dmDataBaseType && tf.Tag.Get("dataType") == "clob" {
					whereSqlFieldNames = append(whereSqlFieldNames, "`"+"text_equal("+mysqlFieldName+",?)"+"`")
				} else {
					whereSqlFieldNames = append(whereSqlFieldNames, "`"+mysqlFieldName+"`=?")
				}
				whereSqlFieldValues = append(whereSqlFieldValues, f.Interface())
				break
			}
		}
	}

	insertSql := fmt.Sprintf("insert into `%s` (%s) values (%s)", tableName, strings.Join(insertSqlFieldNames, ","), strings.Join(insertSqlPlaceHolders, ","))
	if useSqlOnDup {
		insertSql = fmt.Sprintf("%s ON DUPLICATE KEY UPDATE %s", insertSql, strings.Join(updateSqlFieldNames, ","))
		for _, value := range updateSqlFieldValues {
			insertSqlFieldValues = append(insertSqlFieldValues, value)
		}
		// 组装dm的插入更新
		if c.DbType == dmDataBaseType {
			var selectLang, insertNoAutoIncrLang, updateLang, onLang []string
			for _, filed := range updateFields {
				updateLang = append(updateLang, " t1."+filed+"=t2."+filed)
			}

			for i := 0; i < nf; i++ {
				skipTag := false
				tf := tt.Field(i)
				mysqlFieldName := tf.Tag.Get(c.LookTag)
				if mysqlFieldName == "" {
					continue
				}
				selectLang = append(selectLang, fmt.Sprintf("? %s", mysqlFieldName))
				for _, pk := range primaryKeys {
					if pk != "" && mysqlFieldName == pk {
						skipTag = true
						break
					}
				}
				if !skipTag {
					insertNoAutoIncrLang = append(insertNoAutoIncrLang, fmt.Sprintf("t2.%s", mysqlFieldName))
				}
			}
			for _, pk := range primaryKeys {
				onLang = append(onLang, fmt.Sprintf("t1.%s=t2.%s", pk, pk))
			}

			insertSql = "merge into " + tableName + " as t1" +
				" using (select " + strings.Join(selectLang, ",") + " from dual )" + "as t2 on (" + strings.Join(onLang, " and ") + ")" +
				" when matched then update set " + strings.Join(updateLang, ",") +
				" when not matched then insert (" + strings.ReplaceAll(strings.Join(insertNoAutoIncrLang, ","), "t2.", "") + ") values  (" + strings.Join(insertNoAutoIncrLang, ",") + ")"
			insertSql = strings.ReplaceAll(insertSql, "`", "")
			iRows, err := c.Execute(insertSql, insertSqlFieldValues...)
			log.Debug("InsertOrUpdateOnDup use SqlOnDup, table=%s, sql=%s, values=%v, ret=%d, err=%v", tableName, insertSql, insertSqlFieldValues, iRows, err)
			return iRows, err
		} else {
			iRows, err := c.Execute(insertSql, insertSqlFieldValues...)
			log.Debug("InsertOrUpdateOnDup use SqlOnDup, table=%s, sql=%s, values=%v, ret=%d, err=%v", tableName, insertSql, insertSqlFieldValues, iRows, err)
			return iRows, err
		}
	} else {
		updateSql := fmt.Sprintf("update `%s` set %s where %s;", tableName, strings.Join(updateSqlFieldNames, ","), strings.Join(whereSqlFieldNames, " and "))
		for _, value := range whereSqlFieldValues {
			updateSqlFieldValues = append(updateSqlFieldValues, value)
		}
		if c.DbType == dmDataBaseType {
			updateSql = strings.ReplaceAll(updateSql, "`", "")
		}
		uRows, err := c.Execute(updateSql, updateSqlFieldValues...)
		log.Debug("InsertOrUpdateOnDup no use SqlOnDup, table=%s, sql=%s, values=%v, ret=%d, err=%v", tableName, updateSql, updateSqlFieldValues, uRows, err)
		if err != nil {
			return uRows, err
		}

		if uRows == 0 {
			if c.DbType == dmDataBaseType {
				return uRows, errors.New("DM数据库更新数据失败，请指定主键值进行更新操作")
			}
			uRows, err = c.Execute(insertSql, insertSqlFieldValues...)
			log.Debug("InsertOrUpdateOnDup no use SqlOnDup, table=%s, sql=%s, values=%v, ret=%d, err=%v", tableName, insertSql, insertSqlFieldValues, uRows, err)
			if err != nil {
				if mysqlError, ok := err.(*mysql.MySQLError); ok {
					// 1062 is duplicate entry error
					if mysqlError.Number == 1062 {
						log.Debug("InsertOrUpdateOnDup no use SqlOnDup, occur duplicate entry error, error:%v", mysqlError)
						pc.Incr(PcTransactionInsertDup, 1)
						uRows, err = c.Execute(updateSql, updateSqlFieldValues...)
					}
				}
			}
		}

		return uRows, err
	}
}

// Update DM支持该方法 根据condition删除数据 .
//
// condition value supports limit kinds of slice:[]int64,[]string,[]interface
func (c *MysqlClient) Delete(tableName string, condition map[string]interface{}) (int64, error) {
	escapedName := MysqlEscapeString(tableName)
	tableName = escapedName
	if len(condition) <= 0 {
		return 0, errors.New("del with empty condition")
	}
	condStr, dest := buildWhereSql(condition)
	writeDb := c.getWriteDbs()
	sql := "delete from `" + tableName + "` where " + condStr
	if c.DbType == dmDataBaseType {
		sql = strings.ReplaceAll(sql, "`", "")
	}
	rows, err := writeDb.Exec(sql, dest...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := rows.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

func (c *MysqlClient) Execute(sql string, args ...interface{}) (int64, error) {
	writeDb := c.getWriteDbs()
	if c.DbType == dmDataBaseType {
		sql = strings.ReplaceAll(sql, "`", "")
	}
	result, err := writeDb.Exec(sql, args...)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

func (c *MysqlClient) ExecTransaction(transactionExec TransactionExec) (int64, error) {
	writeDb := c.getWriteDbs()
	result, err := writeDb.ExecTransaction(transactionExec)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

func (c *MysqlClient) Getx(dst interface{}, query string, args ...interface{}) error {
	db, err := c.getReadDb()
	if err != nil {
		return err
	}
	return db.DB.Get(dst, query, args...)
}

func (c *MysqlClient) GetContext(ctx context.Context, dst interface{}, query string, args ...interface{}) error {
	db, err := c.getReadDb()
	if err != nil {
		return err
	}
	return db.DB.GetContext(ctx, dst, query, args...)
}

func (c *MysqlClient) Queryx(query string, args ...interface{}) (*sql.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.Query(query, args...)
}

func (c *MysqlClient) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.QueryContext(ctx, query, args...)
}

func (c *MysqlClient) Queryxx(query string, args ...interface{}) (*sqlx.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.Queryx(query, args...)
}

func (c *MysqlClient) QueryxContext(ctx context.Context, query string, args ...interface{}) (*sqlx.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.QueryxContext(ctx, query, args...)
}

func (c *MysqlClient) QueryRow(query string, args ...interface{}) (*sql.Row, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.QueryRow(query, args...), nil
}

func (c *MysqlClient) QueryRowContext(ctx context.Context, query string, args ...interface{}) (*sql.Row, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.QueryRowContext(ctx, query, args...), nil
}

func (c *MysqlClient) QueryRowx(query string, args ...interface{}) (*sqlx.Row, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.QueryRowx(query, args...), nil
}

func (c *MysqlClient) QueryRowxContext(ctx context.Context, query string, args ...interface{}) (*sqlx.Row, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.DB.QueryRowxContext(ctx, query, args...), nil
}

func (c *MysqlClient) Selectx(dst interface{}, query string, args ...interface{}) error {
	db, err := c.getReadDb()
	if err != nil {
		return err
	}
	return db.Select(dst, query, args...)
}

func (c *MysqlClient) SelectContext(ctx context.Context, dst interface{}, query string, args ...interface{}) error {
	db, err := c.getReadDb()
	if err != nil {
		return err
	}
	return db.SelectContext(ctx, dst, query, args...)
}

func (c *MysqlClient) NamedExec(query string, arg interface{}) (sql.Result, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.NamedExec(query, arg)
}

func (c *MysqlClient) NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.NamedExecContext(ctx, query, arg)
}

func (c *MysqlClient) NamedQuery(query string, arg interface{}) (*sqlx.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.NamedQuery(query, arg)
}

func (c *MysqlClient) NamedQueryContext(ctx context.Context, query string, arg interface{}) (*sqlx.Rows, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, err
	}
	return db.NamedQueryContext(ctx, query, arg)
}

func (c *MysqlClient) BindNamed(query string, arg interface{}) (string, []interface{}, error) {
	db, err := c.getReadDb()
	if err != nil {
		return "", nil, err
	}
	return db.BindNamed(query, arg)
}

func (c *MysqlClient) BeginTxx(ctx context.Context, opts *sql.TxOptions) (*sqlx.Tx, error) {
	db, err := c.getWriteDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.BeginTxx(ctx, opts)
}

func (c *MysqlClient) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := c.getWriteDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.BeginTx(ctx, opts)
}

func (c *MysqlClient) Prepare(query string) (*sql.Stmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.Prepare(query)
}

func (c *MysqlClient) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.PrepareContext(ctx, query)
}

func (c *MysqlClient) Preparex(query string) (*sqlx.Stmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.Preparex(query)
}

func (c *MysqlClient) PreparexContext(ctx context.Context, query string) (*sqlx.Stmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.PreparexContext(ctx, query)
}

func (c *MysqlClient) PrepareNamed(query string) (*sqlx.NamedStmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.PrepareNamed(query)
}

func (c *MysqlClient) PrepareNamedContext(ctx context.Context, query string) (*sqlx.NamedStmt, error) {
	db, err := c.getReadDb()
	if err != nil {
		return nil, NoAvailableDBErr
	}
	return db.DB.PrepareNamedContext(ctx, query)
}

func (c *MysqlClient) CreateTable(data interface{}) error {
	if !CheckDataIsStructOrStructPtr(data) {
		return DataTypeErr
	}
	typeOf := reflect.TypeOf(data)
	if typeOf.Kind() == reflect.Ptr {
		typeOf = typeOf.Elem()
	}
	data = reflect.New(typeOf).Interface()
	if createInterface, ok := data.(TableCreateInterface); ok {
		createSql := createInterface.CreateSql()
		if createSql == "" {
			return fmt.Errorf("createSql method return empty")
		}
		_, err := c.getWriteDbs().Exec(createSql)
		if err != nil {
			return err
		}
	}
	return nil
}
