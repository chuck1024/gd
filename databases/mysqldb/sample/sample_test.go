/**
 * Copyright 2019 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package sample

import (
	"github.com/chuck1024/gd"
	"github.com/chuck1024/gd/databases/mysqldb"
	"testing"
)

type TestDB struct {
	Id       uint64 `json:"id" mysqlField:"id" db:"id"`
	Name     string `json:"name" mysqlField:"name" db:"name"`
	CardId   uint64 `json:"card_id" mysqlField:"card_id" db:"card_id"`
	Sex      string `json:"sex" mysqlField:"sex" dataType:"clob" db:"sex"`
	Birthday uint64 `json:"birthday" mysqlField:"birthday" db:"birthday"`
	Status   uint64 `json:"status" mysqlField:"status" db:"status"`
	CreateTs uint64 `json:"create_time" mysqlField:"create_time" db:"create_ts"`
	UpdateTs uint64 `json:"update_time" mysqlField:"update_time" db:"update_ts"`
}

func (t *TestDB) CreateSql() string {
	return `create table if not exists test(
    id int not null AUTO_INCREMENT primary key,
    name varchar(255) not null,
    card_id bigint not null,
    sex varchar(20) default 'man' not null,
    birthday bigint not null,
    status bigint not null,
    create_ts bigint not null,
    update_ts bigint not null
)`
}

func (t *TestDB) TableName() string {
	return "test"
}

func TestDbMethod(t *testing.T) {
	defer gd.LogClose()
	gd.SetConfPath("C:\\Users\\venus\\GolandProjects\\gd\\databases\\mysqldb\\sample\\conf\\conf.ini")
	o := mysqldb.MysqlClient{DataBase: "honeypot", DbConf: gd.GetConfFile(), LookTag: "db"}
	if err := o.Start(); err != nil {
		gd.Error("err:%s", err)
		return
	}

	// Query
	var err error
	err = o.CreateTable(&TestDB{})
	if err != nil {
		gd.Crash(err)
	}

	query := "select ? from test where  id=? limit 1"
	data, err := o.Query((*TestDB)(nil), query, 1)
	if err != nil {
		gd.Error("err:%s", err)
		return
	}

	if data == nil {
		gd.Error("err:%s", err)
		//return
	}

	insert := &TestDB{
		Name:     "chucks",
		CardId:   145251,
		Sex:      "male",
		Birthday: 1312412,
		Status:   1,
		CreateTs: 112131231,
		UpdateTs: 112131231,
	}

	err = o.Add("test", insert, false)
	if err != nil {
		gd.Error("%s", err)
	}

	id, err := o.AddEscapeAutoIncrAndRetLastId("test", insert, "id")
	if err != nil {
		gd.Error("%s", err)
	}
	gd.Info("last id is %d", id)

	insert2 := &TestDB{
		Name:     "xxxxxxx",
		CardId:   1111444443333,
		Sex:      "male",
		Birthday: 1312412,
		Status:   1,
		CreateTs: 112131231,
		UpdateTs: 112131231,
	}

	id, err = o.AddEscapeAutoIncr("test", insert2, true, "id")
	if err != nil {
		gd.Error("%s", err)
	}
	gd.Info("last id is %d", id)

	id, err = o.AddEscapeAutoIncrAndRetLastId("test", insert2, "id")
	if err != nil {
		gd.Error("%s", err)
	}
	gd.Info("last id is %d", id)

	updateData := &TestDB{
		Id:       5,
		Name:     "rqtyhjkl;lgkfjdhg",
		CardId:   4125115,
		Sex:      "1111",
		Birthday: 66666666,
		Status:   1,
		CreateTs: 000000000,
		UpdateTs: 141251,
	}

	primaryKey := []string{"id", "sex"}
	updateFiled := []string{"name", "create_time"}

	_, err = o.InsertOrUpdateOnDup("test", updateData, primaryKey, updateFiled, true)
	if err != nil {
		gd.Error("%s", err)
	}

	//支持
	c, err := o.GetCount("select count(*) from test")
	if err != nil {
		gd.Error("%s", err)
	}
	gd.Info("count %d", c)

	query1 := "select ? from test where sex = ? "
	retList, err := o.QueryList((*TestDB)(nil), query1, "fmale")
	if err != nil {
		gd.Error("query list failed:%v", err)
	}
	testList := make([]*TestDB, 0)
	for _, ret := range retList {
		product, _ := ret.(*TestDB)
		testList = append(testList, product)
		gd.Info("%+v", *product)
	}

	where := make(map[string]interface{})
	where["id"] = 1
	err = o.Update("test", &TestDB{Birthday: 123}, where, []string{"birthday"})
	if err != nil {
		gd.Error("%s", err)
	}

	con := make(map[string]interface{})
	con["name"] = []string{"chucks"}
	_, err = o.Delete("test", con)
	if err != nil {
		gd.Error("%s", err)
	}
	//txx, err := o.BeginTxx(context.Background(), nil)
	//if err != nil {
	//	gd.Error(err)
	//}
	//txx.Prepare("select name from test where id = ?")
	stmt, err := o.Preparex("select name from test where id = ?")
	if err != nil {
		gd.Error(err)
	}
	defer stmt.Close()
	row := stmt.QueryRow(4)
	var res TestDB
	err = row.Scan(&res.Name)
	if err != nil {
		gd.Error(err)
	}
	gd.Info("--------> %s", res.Name)

	db, err := o.GetLBReadDb()
	if err != nil {
		gd.Error(err)
	}
	var count int
	err = db.GetSqlxCon().Get(count, "select count(*) from test where id=?", 1)
	if err != nil {
		gd.Error(err)
	}
	gd.Info(count)
}
