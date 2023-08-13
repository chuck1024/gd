package mysqldb

import "fmt"

var (
	DataTypeErr      = fmt.Errorf("data must be struct or struct ptr")
	NoAvailableDBErr = fmt.Errorf("no available db")
)

type TableName struct {
	TableName string `json:"table_name" mysqlField:"TABLE_NAME"`
}

type TableNameInterface interface {
	TableName() string
}

type TableCreateInterface interface {
	CreateSql() string
}
