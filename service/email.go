package service

import (
	"fmt"
	"zh.xyz/dv/sync/config"
	"zh.xyz/dv/sync/utils"

	"gopkg.in/gomail.v2"
)

// SendConflictNotification 发送冲突通知邮件
func SendConflictNotification(email string, conflictID uint, token string, conflictType string) error {
	// 构建链接（包含token用于身份验证）
	link := fmt.Sprintf("http://your-domain.com/api/v1/conflicts/view?token=%s", token)

	subject := "数据库同步冲突通知"
	body := fmt.Sprintf(`
		<html>
		<body>
			<h2>数据库同步冲突通知</h2>
			<p>检测到数据库同步过程中出现数据冲突：</p>
			<ul>
				<li>冲突ID: %d</li>
				<li>冲突类型: %s</li>
			</ul>
			<p>请点击以下链接查看和处理冲突：</p>
			<p><a href="%s">查看冲突详情</a></p>
			<p>链接有效期：24小时</p>
		</body>
		</html>
	`, conflictID, conflictType, link)

	return sendEmail(email, subject, body)
}

// sendEmail 发送邮件
func sendEmail(to, subject, body string) error {
	cfg := config.GlobalConfig.Email

	m := gomail.NewMessage()
	m.SetHeader("From", cfg.From)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(cfg.Host, cfg.Port, cfg.Username, cfg.Password)

	return d.DialAndSend(m)
}

// GenerateConflictToken 生成冲突查看token（包含冲突ID和用户信息）
func GenerateConflictToken(conflictID uint, userID uint, username string) (string, error) {
	tokenData := map[string]interface{}{
		"conflict_id": conflictID,
		"user_id":     userID,
		"username":    username,
		"type":        "conflict_view",
	}
	
	// 使用自定义token生成
	return utils.GenerateConflictViewToken(tokenData)
}
