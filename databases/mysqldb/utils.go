/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package mysqldb

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/chuck1024/gd/utls"
	"net"
	"net/url"
	"reflect"
	"strings"

	"github.com/go-sql-driver/mysql"
)

func IsDbConnError(err error) bool {
	if err == nil {
		return false
	} else if IsTimeoutError(err) || err == driver.ErrBadConn || err == mysql.ErrInvalidConn {
		return true
	} else if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	} else if strings.Contains(err.Error(), "connection refused") {
		return true
	} else {
		mse, ok := err.(*mysql.MySQLError)
		if ok && mse != nil {
			// 1062 is duplicate entry error
			if mse.Number == 1062 {
				return false
			}

			return true
		}
	}

	return false
}

func GetFieldsName(v interface{}) (res string, errRet error) {
	fieldNamesArray, errRet := GetFieldsNameArray(v)
	if errRet != nil {
		return
	}
	return strings.Join(fieldNamesArray, ","), nil
}

func GetFieldsNameArray(v interface{}) (res []string, errRet error) {
	utls.WithRecover(
		func() {
			res, errRet = _getFieldsNameArray(v)
		},
		func(panicErr interface{}) {
			mixErr := fmt.Errorf("GetFieldsNameArray paniced!panic=%v,origErr=%v", panicErr, errRet)
			errRet = mixErr
		},
	)

	return
}

func _getFieldsNameArray(v interface{}) (res []string, errRet error) {
	res = make([]string, 0)
	typ := reflect.TypeOf(v)
	if typ == nil {
		errRet = fmt.Errorf("input cannot be nil")
		return
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		errRet = fmt.Errorf("not a struct type")
		return
	}
	nf := typ.NumField()
	fieldNamesArray := make([]string, 0, nf)
	for i := 0; i < nf; i++ {
		tf := typ.Field(i)
		mysqlFieldName := tf.Tag.Get("mysqlField")
		if mysqlFieldName == "" {
			pbTags := strings.Split(tf.Tag.Get("protobuf"), ",")
			for _, pbTag := range pbTags {
				nameTag := strings.Split(pbTag, "=")
				if len(nameTag) > 1 && nameTag[0] == "name" && nameTag[1] != "" {
					mysqlFieldName = nameTag[1]
					break
				}
			}
		}
		if mysqlFieldName != "" {
			mysqlFieldName = "`" + mysqlFieldName + "`"
			fieldNamesArray = append(fieldNamesArray, mysqlFieldName)
		}
	}

	return fieldNamesArray, nil
}

func GetFields(v interface{}) (res []interface{}, errRet error) {
	defer func() {
		if r := recover(); r != nil {
			res = nil
			errRet = fmt.Errorf("err when getFields in reflect %v", r)
		}
	}()
	if v == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return nil, fmt.Errorf("input cannot be nil")
	}

	// we need to get the struct value if it is a pointer
	if rv.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("input need to be a pointer")
	}
	rv = rv.Elem()
	num := rv.NumField()
	rt := rv.Type()
	res = make([]interface{}, 0, num)
	for i := 0; i < num; i++ {
		field := rv.Field(i)
		tField := rt.Field(i)
		tag := tField.Tag
		if tag.Get("mysqlField") != "" || tag.Get("protobuf") != "" {
			if !field.CanAddr() {
				return nil, fmt.Errorf("%v cannot be address", tField.Name)
			}
			res = append(res, field.Addr().Interface())
		}
	}
	return
}

func buildWhereSql(data map[string]interface{}) (string, []interface{}) {
	placeholder := make([]string, 0, len(data))
	valHolder := make([]interface{}, 0)

	for idx, v := range data {
		k := MysqlEscapeString(idx)
		switch val := v.(type) {
		case []interface{}:
			valHolder = append(valHolder, val...)
			str := fmt.Sprintf(" `%s` in %s ", k, makeSqlPlaceHolderForIn(val))
			placeholder = append(placeholder, str)
		case []int64:
			temvarHolder := make([]interface{}, 0, len(val))
			for _, v := range val {
				temvarHolder = append(temvarHolder, v)
			}
			valHolder = append(valHolder, temvarHolder...)
			str := fmt.Sprintf(" `%s` in %s ", k, makeSqlPlaceHolderForIn(temvarHolder))
			placeholder = append(placeholder, str)
		case []string:
			temvarHolder := make([]interface{}, 0, len(val))
			for _, v := range val {
				temvarHolder = append(temvarHolder, v)
			}
			valHolder = append(valHolder, temvarHolder...)
			str := fmt.Sprintf(" `%s` in %s ", k, makeSqlPlaceHolderForIn(temvarHolder))
			placeholder = append(placeholder, str)
		default:
			valHolder = append(valHolder, val)
			str := fmt.Sprintf(" `%s` = ? ", k)
			placeholder = append(placeholder, str)
		}
	}
	return strings.Join(placeholder, "AND"), valHolder
}

func makeSqlPlaceHolderForIn(vals []interface{}) string {
	buffer := bytes.NewBufferString("(")
	for k := range vals {
		buffer.WriteString("?")
		if k != len(vals)-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString(")")
	return buffer.String()
}

type SqlCondition struct {
	tableprefix string
	conds       []*condition
	order       []*orderBy
	offset      int64
	limit       int64
}

type condition struct {
	key     string
	compare string
	value   interface{}
}

type orderBy struct {
	key  string
	desc bool
}

func NewSqlCondition() *SqlCondition {
	return &SqlCondition{
		conds: []*condition{},
		order: []*orderBy{},
		limit: 300,
	}
}

func (c *SqlCondition) WithTablePrefix(tablePrefix string) *SqlCondition {
	c.tableprefix = MysqlEscapeString(tablePrefix)
	return c
}

func (c *SqlCondition) WithCondition(key, compare string, value interface{}) *SqlCondition {
	key = strings.Trim(key, " ")
	if key != "" {
		compare = strings.Trim(compare, " ")
		switch compare {
		case "=", ">", ">=", "<", "<=", "<>":
		default:
			compare = "="
		}
		c.conds = append(c.conds, &condition{
			key:     MysqlEscapeString(key),
			compare: compare,
			value:   value,
		})
	}
	return c
}

func (c *SqlCondition) WithOrder(key string, isdesc bool) *SqlCondition {
	key = strings.Trim(key, " ")
	key = "`" + key + "`"
	if key != "" {
		c.order = append(c.order, &orderBy{
			key:  MysqlEscapeString(key),
			desc: isdesc,
		})
	}
	return c
}

func (c *SqlCondition) WithLimit(limit int64) *SqlCondition {
	if limit >= 0 {
		c.limit = limit
	}
	return c
}

func (c *SqlCondition) WithOffset(offset int64) *SqlCondition {
	if offset > 0 {
		c.offset = offset
	}
	return c
}

/*
Valid SqlCondition,use before buildSql
*/
func (c *SqlCondition) Valid(isGet bool) error {
	if !isGet {
		if len(c.conds) == 0 {
			return errors.New("cant alter all table record once")
		}
	} else {
		if c.limit == 0 && len(c.conds) == 0 {
			return errors.New("cant load all table record once")
		}
	}
	return nil
}

/*
		returns:
		@sqlStr string      contains all contents after(include) WHERE
	    @vars   []interface	varsHolder
*/
func (c *SqlCondition) BuildWhereSql() (string, []interface{}) {
	_, str, vars := c.BuildShardWhereSql("")
	return str, vars
}

/*
		Need to call WithTablePrefix() first
		returns:
		@tableName	string	tableName
		@sqlStr string      contains all contents after(include) WHERE
	    @vars	[]interface	varsHolder
*/
func (c *SqlCondition) BuildShardWhereSql(shardKey string) (string, string, []interface{}) {
	tableName := MysqlEscapeString(c.tableprefix + shardKey)
	valHolder := make([]interface{}, 0)
	placeholder := make([]string, 0, len(c.conds))

	for _, v := range c.conds {
		k := v.key
		switch val := v.value.(type) {
		case []interface{}:
			valHolder = append(valHolder, val...)
			str := fmt.Sprintf(" `%s` IN %s", k, makeSqlPlaceHolderForIn(val))
			placeholder = append(placeholder, str)
		case []int64:
			temvarHolder := make([]interface{}, 0, len(val))
			for _, v := range val {
				temvarHolder = append(temvarHolder, v)
			}
			valHolder = append(valHolder, temvarHolder...)
			str := fmt.Sprintf(" `%s` IN %s", k, makeSqlPlaceHolderForIn(temvarHolder))
			placeholder = append(placeholder, str)
		case []string:
			temvarHolder := make([]interface{}, 0, len(val))
			for _, v := range val {
				temvarHolder = append(temvarHolder, v)
			}
			valHolder = append(valHolder, temvarHolder...)
			str := fmt.Sprintf(" `%s` IN %s", k, makeSqlPlaceHolderForIn(temvarHolder))
			placeholder = append(placeholder, str)
		default:
			valHolder = append(valHolder, val)
			if v.compare == "" {
				v.compare = "="
			}
			str := fmt.Sprintf(" `%s` %s ?", k, v.compare)
			placeholder = append(placeholder, str)
		}
	}
	condStr := strings.Join(placeholder, " AND")
	// build order by
	if len(c.order) != 0 {
		orderHolder := make([]string, 0, len(c.order))
		condStr = condStr + " ORDER BY "
		for _, v := range c.order {
			buffer := bytes.NewBufferString(v.key)
			if v.desc {
				buffer.WriteString(" DESC")
			} else {
				buffer.WriteString(" ASC")
			}
			orderHolder = append(orderHolder, buffer.String())
		}
		condStr = condStr + strings.Join(orderHolder, ",")
	}

	// build limit
	if c.limit > 0 {
		condStr = condStr + " LIMIT ?"
		if c.offset > 0 {
			condStr = condStr + ",?"
			valHolder = append(valHolder, interface{}(c.offset))
		}
		valHolder = append(valHolder, interface{}(c.limit))
	}

	if condStr != "" && (!strings.HasPrefix(condStr, " LIMIT ?") || !strings.HasPrefix(condStr, " ORDER BY ")) {
		condStr = " WHERE" + condStr
	}
	return tableName, condStr, valHolder
}

func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	switch err := err.(type) {
	case *url.Error:
		if err, ok := err.Err.(net.Error); ok && err.Timeout() {
			return true
		}
	case net.Error:
		if err.Timeout() {
			return true
		}
	default:
	}

	return false
}

func MysqlEscapeString(source string) string {
	var j int = 0
	if len(source) == 0 {
		return ""
	}
	tempStr := source[:]
	desc := make([]byte, len(tempStr)*2)
	for i := 0; i < len(tempStr); i++ {
		var escape byte
		escape = 0
		switch tempStr[i] {
		case 0:
			escape = '0'
		case '\r':
			escape = 'r'
		case '\n':
			escape = 'n'
		case '\\':
			escape = '\\'
		case '\'':
			escape = '\''
		case '"':
			escape = '"'
		case '\032':
			escape = 'Z'
		}
		if escape != 0 {
			desc[j] = '\\'
			desc[j+1] = escape
			j = j + 2
		} else {
			desc[j] = tempStr[i]
			j = j + 1
		}
	}
	return string(desc[0:j])
}

func GetDataStructFields(data interface{}, tagName string) ([]string, error) {
	if tagName == "" {
		return nil, fmt.Errorf("tagName is empty")
	}
	t := reflect.TypeOf(data)
	var typeOf reflect.Type
	if t.Kind() == reflect.Struct {
		typeOf = reflect.TypeOf(data)
	} else if t.Elem().Kind() == reflect.Struct {
		typeOf = reflect.TypeOf(data).Elem()
	} else {
		return nil, fmt.Errorf("data must struct or struct ptr")
	}
	numField := typeOf.NumField()
	fieldSlice := make([]string, 0, numField)
	for i := 0; i < numField; i++ {
		tField := typeOf.Field(i)
		if len(tField.PkgPath) > 0 {
			return nil, fmt.Errorf("field %s is not public", tField.Name)
		}
		mysqlFieldName := tField.Tag.Get(tagName)
		if len(mysqlFieldName) == 0 {
			return nil, fmt.Errorf("field %s has no %s tag", tField.Name, tagName)
		}
		fieldSlice = append(fieldSlice, "`"+mysqlFieldName+"`")
	}
	return fieldSlice, nil
}

func GetDataStructValues(data interface{}) []driver.Value {
	valueOf := reflect.ValueOf(data).Elem()
	numField := reflect.TypeOf(data).Elem().NumField()
	valueSlice := make([]driver.Value, 0, numField)
	for i := 0; i < numField; i++ {
		valueSlice = append(valueSlice, valueOf.Field(i).Interface())
	}
	return valueSlice
}

func GetDataStructDests(data interface{}, dbType string, tagName string) ([]interface{}, map[int]int, error) {
	typeOf := reflect.TypeOf(data).Elem()
	valueOf := reflect.ValueOf(data).Elem()
	numField := valueOf.NumField()
	dests := make([]interface{}, 0, numField)
	indexs := make(map[int]int)
	for i := 0; i < numField; i++ {
		tField := typeOf.Field(i)
		if len(tField.PkgPath) > 0 {
			return nil, nil, fmt.Errorf("field %s is not public", tField.Name)
		}
		mysqlFieldName := tField.Tag.Get(tagName)
		if len(mysqlFieldName) == 0 {
			return nil, nil, fmt.Errorf("field %s has no %s tag", tField.Name, tagName)
		}

		if dbType == dmDataBaseType && tField.Tag.Get("dataType") == "clob" {
			indexs[i] = i
		}

		vField := valueOf.Field(i)
		if vField.CanAddr() {
			dests = append(dests, vField.Addr().Interface())
		} else {
			return nil, nil, fmt.Errorf("%v can not be addressed", vField)
		}
	}
	return dests, indexs, nil
}

func CheckDataIsStructOrStructPtr(data interface{}) bool {
	t := reflect.TypeOf(data)
	if t.Kind() == reflect.Struct || t.Elem().Kind() == reflect.Struct {
		return true
	} else {
		return false
	}
}
