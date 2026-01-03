package utils

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// GenerateConflictViewToken 生成冲突查看token（简化版本，实际可以使用JWT）
func GenerateConflictViewToken(data map[string]interface{}) (string, error) {
	data["exp"] = time.Now().Add(24 * time.Hour).Unix()
	
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	
	// Base64编码（实际应用中应该加密）
	return base64.URLEncoding.EncodeToString(jsonData), nil
}

// ParseConflictViewToken 解析冲突查看token
func ParseConflictViewToken(token string) (map[string]interface{}, error) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	
	// 检查过期时间
	if exp, ok := result["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, ErrTokenExpired
		}
	}
	
	return result, nil
}

var ErrTokenExpired = &TokenError{Message: "token已过期"}

type TokenError struct {
	Message string
}

func (e *TokenError) Error() string {
	return e.Message
}
