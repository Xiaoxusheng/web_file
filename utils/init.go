package utils

import "C"
import (
	"encoding/json"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"time"
)

type MysqlConfig struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	Url             string `json:"url"`
	Database        string `json:"database"`
	Port            int32  `json:"port"`
	MaxIdleConn     int    `json:"maxIdleConn"`
	MaxOpenCons     int    `json:"maxOpenCons"`
	ConnMaxLifetime int    `json:"connMaxLifetime"`
}

var DB *gorm.DB

func InitMysql() {
	mysqlconfig := new(MysqlConfig)
	file, err := os.OpenFile("./config.json", os.O_RDWR, 0644)
	if err != nil {
		log.Println("读取json失败，err:", err)
		return
	}
	err = json.NewDecoder(file).Decode(mysqlconfig)
	if err != nil {
		log.Println(err)
		return

	}

	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second, // Slow SQL threshold
			LogLevel:                  logger.Info, // Log level
			IgnoreRecordNotFoundError: true,        // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      true,        // Don't include params in the SQL log
			Colorful:                  false,       // Disable color
		},
	)
	dsn := mysqlconfig.Username + ":" + mysqlconfig.Password + "@tcp(" + mysqlconfig.Url + ")/" + mysqlconfig.Database + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:      newLogger,
		PrepareStmt: true,
	})
	if err != nil {
		panic(err)
	}
	db.Debug()
	log.Println("mysql连接成功！")
	mysqlDB, err := db.DB()
	if err != nil {
		panic(err)
	}
	// SetMaxIdleConns 用于设置连接池中空闲连接的最大数量。
	mysqlDB.SetMaxIdleConns(mysqlconfig.MaxIdleConn)

	// SetMaxOpenConns 设置打开数据库连接的最大数量。
	mysqlDB.SetMaxOpenConns(mysqlconfig.MaxOpenCons)

	// SetConnMaxLifetime 设置了连接可复用的最大时间。
	mysqlDB.SetConnMaxLifetime(time.Minute * time.Duration(mysqlconfig.ConnMaxLifetime))
	DB = db
}
