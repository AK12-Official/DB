package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	JWT      JWTConfig      `json:"jwt"`
	Email    EmailConfig    `json:"email"`
}

type ServerConfig struct {
	Port string `json:"port"`
	Mode string `json:"mode"` // debug, release
}

type DatabaseConfig struct {
	Type     string `json:"type"` // mysql, postgres, sqlite
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
}

type JWTConfig struct {
	Secret     string `json:"secret"`
	ExpireTime int    `json:"expire_time"` // 小时
}

type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

var GlobalConfig *Config

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		// 如果配置文件不存在，使用默认配置
		// GlobalConfig = &Config{
		// 	Server: ServerConfig{
		// 		Port: "8080",
		// 		Mode: "debug",
		// 	},
		// 	Database: DatabaseConfig{
		// 		Type:     "mysql",
		// 		Host:     "localhost",
		// 		Port:     "3306",
		// 		User:     "root",
		// 		Password: "root",
		// 		DBName:   "db_sync",
		// 	},
		// 	JWT: JWTConfig{
		// 		Secret:     "your-secret-key-change-in-production",
		// 		ExpireTime: 24,
		// 	},
		// 	Email: EmailConfig{
		// 		Host:     "smtp.example.com",
		// 		Port:     587,
		// 		Username: "your-email@example.com",
		// 		Password: "your-password",
		// 		From:     "your-email@example.com",
		// 	},
		// }
		return nil
	}

	GlobalConfig = &Config{}
	return json.Unmarshal(data, GlobalConfig)
}
