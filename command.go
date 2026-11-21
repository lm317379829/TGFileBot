package main

import (
	"errors"
	"fmt"
	"html"
	"log"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xAmarnath/gogram/telegram"
)

// handleBotCommand 是 Bot 的总消息分发入口，处理所有管理指令
func handleBotCommand(m *telegram.NewMessage) error {
	return dispatchBotCommand(m, sendMS, func(message *telegram.NewMessage) {
		safeGo(func() {
			time.Sleep(60 * time.Second)
			if _, err := message.Delete(); err != nil {
				log.Printf("删除命令消息失败: %+v", err)
			}
		})
	})
}

// Keep Telegram sends and delayed deletion at the boundary so command decisions
// and persisted changes can be verified without a live Telegram account.
func dispatchBotCommand(m *telegram.NewMessage, sendMS func(*telegram.NewMessage, any, *telegram.SendOptions, ...int), scheduleDelete func(*telegram.NewMessage)) error {
	if m == nil || m.Message == nil {
		return nil
	}
	needAdmin := func(message *telegram.NewMessage) bool {
		if infos.isAdmin(message.SenderID()) {
			return true
		}
		sendMS(message, "你没有使用此命令的权限", nil, 60)
		return false
	}
	// 该函数由 gogram 在收到消息时异步回调。goroutine panic 不受 http handler
	// 的内置 recover 保护，若此处未恢复会直接终止整个进程（整个机器人退出）。
	// 在入口恢复并记录栈，使单条消息/单条命令处理异常不拖垮服务。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("handleBotCommand panic 已恢复: %v\n%s", r, debug.Stack())
		}
	}()

	// 频道广播消息（bot 为频道成员时收到）的 Sender 可能为 nil, 直接解引用会 panic
	if m.Sender != nil && m.Sender.ID == infos.BotID {
		return nil
	}

	text := strings.TrimSpace(m.Text())
	// 整个命令处理过程使用同一份配置快照, 保证一次命令内多次读取互相一致
	conf := infos.Conf.Load()

	// 拦截非管理指令并匹配正则过滤规则 [FEAT-002]
	// 仅在群组/频道中生效: 私聊里 bot 无权删除用户消息, 且不应干预用户与 bot 的正常对话
	if !m.IsMedia() && text != "" && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "http") && m.SenderID() != 0 && !infos.isWhite(m.SenderID()) && (m.IsGroup() || m.IsChannel()) {
		infos.Mutex.RLock()
		rexRules := infos.RexRules
		infos.Mutex.RUnlock()

		if len(rexRules) > 0 {
			for _, rexRule := range rexRules {
				if rexRule.MatchString(text) {
					if _, err := m.Delete(); err != nil {
						log.Printf("删除群组匹配消息失败: %+v", err)
					}
					return nil
				}
			}
		}
	}

	command, args := parseBotCommand(text, infos.BotUsername)
	if strings.HasPrefix(text, "/") && command == "" {
		return nil
	}
	if command != "" {
		text = command
		if args != "" {
			text += " " + args
		}
	}

	// Recognized command tokens retain the existing delayed deletion behavior.
	if command != "" && scheduleDelete != nil {
		scheduleDelete(m)
	}

	if m.Channel == nil {
		switch command {
		case "/start":
			if !infos.isWhite(m.SenderID()) {
				sendMS(m, "你没有使用此机器人的权限", nil, 60)
				return nil
			}

			var src string
			if m.SenderID() == conf.UserID {
				switch infos.Status.Load() {
				case 0:
					src = "userBot 未登录, 仅使用 Bot 或发送 /phone 手机号登录 userBot"
				case 1:
					src = "正在等待验证码, 请发送 /code 验证码"
				case 2:
					src = "正在等待密码, 请发送 /pass 密码"
				case 3:
					src = "userBot 已登录"
				}
			} else {
				src = "仅限内部使用, 请保管好你的HASH密码与UID"
			}
			sendMS(m, src, nil)
			return nil
		case "/allow":
			if !needAdmin(m) {
				return nil
			}
			whiteID, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(text, "/allow")), 10, 64)
			if err != nil {
				sendMS(m, fmt.Sprintf("添加白名单失败: %+v", err), nil, 60)
				return nil
			}

			if whiteID != 0 {
				added := false
				if err := infos.refreshConf(func(c *Conf) {
					if !slices.Contains(c.WhiteIDs, whiteID) {
						c.WhiteIDs = append(c.WhiteIDs, whiteID)
						added = true
					}
				}); err != nil {
					log.Printf("保存配置文件失败: %+v", err)
					sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
					return nil
				}

				if !added {
					sendMS(m, fmt.Sprintf("白名单中已存在: %d", whiteID), nil, 60)
					return nil
				}

				sendMS(m, fmt.Sprintf("添加白名单成功: %d", whiteID), nil, 60)
			}
			return nil
		case "/disallow":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/disallow"))
			if content == "" {
				sendMS(m, "请提供要移除的白名单索引（如 #0）或用户 ID", nil, 60)
				return nil
			}

			// 用 # 前缀明确区分"按索引删除"与"按 ID 删除", 避免 ID 恰好等于某个下标时的歧义
			indexStr, byIndex := strings.CutPrefix(content, "#")
			var index int
			var num int64
			var err error
			if byIndex {
				index, err = strconv.Atoi(indexStr)
			} else {
				num, err = strconv.ParseInt(content, 10, 64)
			}
			if err != nil {
				sendMS(m, fmt.Sprintf("移除白名单失败: %+v", err), nil, 60)
				return nil
			}

			var whiteID int64
			var successMes string
			found := false
			if err := infos.refreshConf(func(c *Conf) {
				if byIndex {
					if index >= 0 && index < len(c.WhiteIDs) {
						whiteID = c.WhiteIDs[index]
						successMes = fmt.Sprintf("按索引移除白名单成功: %d", whiteID)
						found = true
					}
				} else if num != 0 && slices.Contains(c.WhiteIDs, num) {
					whiteID = num
					successMes = fmt.Sprintf("按ID移除白名单成功: %d", whiteID)
					found = true
				}
				if found {
					c.WhiteIDs = slices.DeleteFunc(c.WhiteIDs, func(v int64) bool {
						return v == whiteID
					})
				}
			}); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}

			if !found {
				switch {
				case byIndex:
					sendMS(m, fmt.Sprintf("索引 #%d 超出白名单范围", index), nil, 60)
				case num == 0:
					return nil
				default:
					sendMS(m, fmt.Sprintf("用户 %d 不在白名单中", num), nil, 60)
				}
				return nil
			}

			sendMS(m, successMes, nil, 60)
			return nil
		case "/qr":
			if m.SenderID() != conf.UserID {
				sendMS(m, "你没有使用此命令的权限", nil, 60)
				return nil
			}
			if err := infos.startUserBotQR(); err != nil {
				sendMS(m, fmt.Sprintf("启动 QR 登录失败: %+v", err), nil, 60)
			}
			return nil
		case "/phone":
			if m.SenderID() != conf.UserID {
				sendMS(m, "你没有使用此命令的权限", nil, 60)
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/phone"))
			if content == "" {
				sendMS(m, "手机不能为空", nil, 60)
				return nil
			}
			if !strings.HasPrefix(content, "+") {
				content = "+" + content
			}
			if err := infos.startUserBot(content); err != nil {
				sendMS(m, fmt.Sprintf("启动 UserBot 失败: %+v", err), nil, 60)
			}
			return nil
		case "/code":
			if m.SenderID() != conf.UserID {
				sendMS(m, "你没有使用此命令的权限", nil, 60)
				return nil
			}
			code := strings.TrimSpace(strings.TrimPrefix(text, "/code"))
			if code == "" {
				sendMS(m, "验证码不能为空", nil, 60)
				return nil
			}
			if err := infos.submitCode(code); err != nil {
				sendMS(m, fmt.Sprintf("提交验证码失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, "提交验证码成功", nil, 60)
			return nil
		case "/pass":
			if m.SenderID() != conf.UserID {
				sendMS(m, "你没有使用此命令的权限", nil, 60)
				return nil
			}
			pass := strings.TrimSpace(strings.TrimPrefix(text, "/pass"))
			if pass == "" {
				sendMS(m, "2FA密码不能为空", nil, 60)
				return nil
			}
			if err := infos.submitPass(pass); err != nil {
				sendMS(m, fmt.Sprintf("提交2FA密码失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, "提交2FA密码成功", nil, 60)
			return nil
		case "/dc":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/dc"))
			if content == "" {
				if conf.DC != 0 {
					sendMS(m, fmt.Sprintf("当前DC: %d", conf.DC), nil, 60)
				} else {
					sendMS(m, "当前未手动指定DC", nil, 60)
				}
				return nil
			}
			value, err := strconv.Atoi(content)
			if err != nil {
				sendMS(m, fmt.Sprintf("DC格式错误: %+v", err), nil, 60)
				return nil
			}
			if value < 1 || value > 5 {
				sendMS(m, "DC必须在1-5之间", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.DC = value }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("DC已设置为: %d, 重启后生效", value), nil, 60)
			return nil
		case "/site":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/site"))
			if content == "" {
				sendMS(m, fmt.Sprintf("当前反代地址: %s", html.EscapeString(conf.Site)), nil, 60)
				return nil
			}
			if _, err := buildStreamLink(content, StreamLinkParams{}); err != nil {
				sendMS(m, "反代地址格式错误", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.Site = content }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("反代地址已设置为: %s", html.EscapeString(content)), nil, 60)
			return nil
		case "/size":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/size"))
			if content == "" {
				sendMS(m, fmt.Sprintf("当前最大缓存: %s", formatFileSize(conf.MaxSize)), nil, 60)
				return nil
			}
			value, err := convertSize(content)
			if err != nil || value <= 0 {
				sendMS(m, "最大缓存格式错误", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.MaxSize = value }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			src := fmt.Sprintf("最大缓存已设置为: %s", formatFileSize(value))
			if value > 128*1024*1024 {
				src += ", 当前缓存较大, 容易引起OOM, 请谨慎设置"
			}
			sendMS(m, src, nil, 60)
			return nil
		case "/proxy":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/proxy"))
			if content == "" {
				if conf.Proxy == "" {
					sendMS(m, "当前未设置代理", nil, 60)
					return nil
				} else {
					sendMS(m, fmt.Sprintf("当前代理: %s", html.EscapeString(conf.Proxy)), nil, 60)
					return nil
				}
			}
			if content == "off" {
				content = ""
			}
			if _, err := telegram.ProxyFromURL(content); err != nil {
				sendMS(m, "代理地址格式错误", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.Proxy = content }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("代理已设置为: %s", html.EscapeString(content)), nil, 60)
			return nil
		case "/password":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/password"))
			if content == "" {
				sendMS(m, "当前密码已设置", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.Password = content }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, "密码已更新", nil, 60)
			return nil
		case "/channel":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/channel"))
			if content == "" {
				sendMS(m, fmt.Sprintf("当前频道ID: %d", conf.ChannelID), nil, 60)
				return nil
			}
			if !strings.HasPrefix(content, "-100") {
				content = "-100" + content
			}
			value, err := strconv.ParseInt(content, 10, 64)
			if err != nil {
				sendMS(m, fmt.Sprintf("频道ID格式错误: %+v", err), nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.ChannelID = value }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("频道ID已设置为: %d", value), nil, 60)
			return nil
		case "/workers":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/workers"))
			if content == "" {
				sendMS(m, fmt.Sprintf("当前并发数: %d", conf.Workers), nil, 60)
				return nil
			}
			num, err := strconv.Atoi(content)
			if err != nil {
				sendMS(m, "并发数必须为数字", nil, 60)
				return nil
			}
			if num < 1 || num > 4 {
				sendMS(m, "并发数必须在 1-4 之间", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.Workers = num }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			src := fmt.Sprintf("并发数已设置为: %d", num)
			sendMS(m, src, nil, 60)
			return nil
		case "/check":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/check"))
			if content == "" {
				sendMS(m, "请提供要检查的哈希值", nil, 60)
				return nil
			}
			if uid := infos.checkHash(content); uid != 0 {
				user, err := infos.BotClient.Load().GetUser(uid)
				if err != nil {
					log.Printf("获取用户信息失败: %+v", err)
					return nil
				}
				fullName := user.FirstName + user.LastName
				var values strings.Builder
				values.WriteString(fmt.Sprintf("• <b>用户 ID</b>: <code>%d</code>\n", uid))
				if fullName != "" {
					values.WriteString(fmt.Sprintf("• <b>显示名称</b>: %s\n", html.EscapeString(fullName)))
				}
				if user.Username != "" {
					values.WriteString(fmt.Sprintf("• <b>用户昵称</b>: @%s\n", user.Username))
				}
				sendMS(m, values.String(), nil, 60)
			}
			return nil
		case "/add":
			if !needAdmin(m) {
				return nil
			}
			channel := strings.TrimSpace(strings.TrimPrefix(text, "/add"))
			if channel == "" {
				sendMS(m, "请提供要添加的频道别名", nil, 60)
				return nil
			}
			channel = strings.TrimPrefix(channel, "@")

			added := false
			if err := infos.refreshConf(func(c *Conf) {
				if !slices.Contains(c.Channels, channel) {
					c.Channels = append(c.Channels, channel)
					added = true
				}
			}); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}

			if !added {
				sendMS(m, fmt.Sprintf("频道 %s 已存在", html.EscapeString(channel)), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("添加频道成功: %s", html.EscapeString(channel)), nil, 60)
			return nil
		case "/del":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/del"))
			if content == "" {
				sendMS(m, "请提供要移除的频道索引（如 #0）或别名", nil, 60)
				return nil
			}

			// 用 # 前缀明确区分"按索引删除"与"按别名删除", 避免别名恰好是纯数字时的歧义
			indexStr, byIndex := strings.CutPrefix(content, "#")
			var index int
			var indexErr error
			channelByName := strings.TrimPrefix(content, "@")
			if byIndex {
				index, indexErr = strconv.Atoi(indexStr)
			}

			var removed, successMes string
			found := false
			if err := infos.refreshConf(func(c *Conf) {
				if byIndex {
					if indexErr == nil && index >= 0 && index < len(c.Channels) {
						removed = c.Channels[index]
						successMes = fmt.Sprintf("按索引移除频道成功: %s", html.EscapeString(removed))
						found = true
					}
				} else if slices.Contains(c.Channels, channelByName) {
					removed = channelByName
					successMes = fmt.Sprintf("按内容移除频道成功: %s", html.EscapeString(channelByName))
					found = true
				}
				if found {
					c.Channels = slices.DeleteFunc(c.Channels, func(key string) bool {
						return key == removed
					})
				}
			}); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}

			if !found {
				if byIndex {
					if indexErr != nil {
						sendMS(m, fmt.Sprintf("索引格式错误: %+v", indexErr), nil, 60)
					} else {
						sendMS(m, fmt.Sprintf("索引 #%d 超出频道列表范围", index), nil, 60)
					}
				} else {
					sendMS(m, fmt.Sprintf("频道 %s 不在搜索列表中", html.EscapeString(channelByName)), nil, 60)
				}
				return nil
			}
			sendMS(m, successMes, nil, 60)
			return nil
		case "/list":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/list"))
			if content == "" {
				sendMS(m, "请提供要列出的类别: <code>channels</code> | <code>rules</code> | <code>ids</code>", nil, 60)
				return nil
			}
			switch content {
			case "channels":
				var values strings.Builder
				count := len(conf.Channels)
				if count == 0 {
					sendMS(m, "⚠️ <b>暂无搜索频道别名</b>", nil, 60)
					break
				}
				values.WriteString(fmt.Sprintf("🔍 <b>搜索频道别名列表</b> (共 %d 个)\n", count))
				values.WriteString("━━━━━━━━━━━━━━━\n")
				for num, ch := range conf.Channels {
					if !strings.HasPrefix(ch, "@") {
						ch = "@" + ch
					}
					values.WriteString(fmt.Sprintf("#%d. %s\n", num, html.EscapeString(ch)))
				}
				sendMS(m, values.String(), nil, 60)
			case "ids":
				var values strings.Builder
				count := len(conf.WhiteIDs)
				if count == 0 {
					sendMS(m, "⚠️ <b>白名单目前为空</b>", nil, 60)
					break
				}
				values.WriteString(fmt.Sprintf("🛡️ <b>白名单 ID 列表</b> (共 %d 个)\n", count))
				values.WriteString("━━━━━━━━━━━━━━━\n")
				for num, whiteID := range conf.WhiteIDs {
					values.WriteString(fmt.Sprintf("#%d. <code>%d</code>\n", num, whiteID))
				}
				sendMS(m, values.String(), nil, 60)
			case "rules":
				var values strings.Builder
				count := len(conf.Rules)
				if count == 0 {
					sendMS(m, "⚠️ <b>目前暂无正则过滤规则</b>", nil, 60)
					break
				}
				values.WriteString(fmt.Sprintf("🚫 <b>正则过滤规则列表</b> (共 %d 个)\n", count))
				values.WriteString("━━━━━━━━━━━━━━━\n")
				for num, rule := range conf.Rules {
					values.WriteString(fmt.Sprintf("#%d. <code>%s</code>\n", num, html.EscapeString(rule)))
				}
				sendMS(m, values.String(), nil, 60)
			default:
				sendMS(m, "类别错误", nil, 60)
			}
			return nil
		case "/port":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/port"))
			if content == "" {
				sendMS(m, "请提供要修改的端口", nil, 60)
				return nil
			}
			value, err := strconv.Atoi(content)
			if err != nil {
				sendMS(m, "端口格式错误", nil, 60)
				return nil
			}
			if value <= 0 || value > 65535 {
				sendMS(m, "端口必须在 1-65535 之间", nil, 60)
				return nil
			}
			if err := infos.refreshConf(func(c *Conf) { c.Port = value }); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}
			sendMS(m, fmt.Sprintf("端口已设置为: %d, 重启后生效", value), nil, 60)
			return nil
		case "/info":
			if !needAdmin(m) {
				return nil
			}

			num := 10
			content := strings.TrimSpace(strings.TrimPrefix(text, "/info"))
			if content != "" {
				src, value := extractContent(content)
				if value != nil {
					num = *value
				}
				content = src
			}

			if num <= 0 {
				sendMS(m, "行数必须大于 0", nil, 60)
				return nil
			}
			if infos.FilePath == "" {
				sendMS(m, "暂未开启日志记录", nil, 60)
				return nil
			}

			lines, err := readLastLines(infos.FilePath, content, num)
			if err != nil {
				sendMS(m, fmt.Sprintf("读取日志失败: %+v", err), nil, 60)
				return nil
			}

			if len(lines) == 0 {
				sendMS(m, "暂无日志内容", nil, 60)
				return nil
			}

			for _, page := range logReplyPages(lines) {
				sendMS(m, page, &telegram.SendOptions{ParseMode: "html"})
			}
			return nil
		case "/addrule":
			if !needAdmin(m) {
				return nil
			}
			rule := strings.TrimSpace(strings.TrimPrefix(text, "/addrule"))
			if rule == "" {
				sendMS(m, "请提供要添加的正则表达式", nil, 60)
				return nil
			}
			if _, err := regexp.Compile(rule); err != nil {
				sendMS(m, fmt.Sprintf("正则表达式格式错误: %+v", err), nil, 60)
				return nil
			}

			added := false
			if err := infos.refreshConf(func(c *Conf) {
				if !slices.Contains(c.Rules, rule) {
					c.Rules = append(c.Rules, rule)
					added = true
				}
			}); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}

			if !added {
				sendMS(m, "该规则已存在", nil, 60)
				return nil
			}
			infos.buildRexRules()
			sendMS(m, "添加正则规则成功", nil, 60)
			return nil
		case "/delrule":
			if !needAdmin(m) {
				return nil
			}
			content := strings.TrimSpace(strings.TrimPrefix(text, "/delrule"))
			if content == "" {
				sendMS(m, "请提供要移除的规则索引（如 #0）或内容", nil, 60)
				return nil
			}

			// 用 # 前缀明确区分"按索引删除"与"按内容删除", 避免规则内容恰好是纯数字时的歧义
			indexStr, byIndex := strings.CutPrefix(content, "#")
			var index int
			var indexErr error
			if byIndex {
				index, indexErr = strconv.Atoi(indexStr)
			}

			var removed, successMes string
			found := false
			if err := infos.refreshConf(func(c *Conf) {
				if byIndex {
					if indexErr == nil && index >= 0 && index < len(c.Rules) {
						removed = c.Rules[index]
						successMes = fmt.Sprintf("按索引移除规则成功: %s", html.EscapeString(removed))
						found = true
					}
				} else if slices.Contains(c.Rules, content) {
					removed = content
					successMes = "按内容移除规则成功"
					found = true
				}
				if found {
					c.Rules = slices.DeleteFunc(c.Rules, func(r string) bool {
						return r == removed
					})
				}
			}); err != nil {
				log.Printf("保存配置文件失败: %+v", err)
				sendMS(m, fmt.Sprintf("保存配置失败: %+v", err), nil, 60)
				return nil
			}

			if !found {
				if byIndex && indexErr != nil {
					sendMS(m, fmt.Sprintf("索引格式错误: %+v", indexErr), nil, 60)
				} else {
					sendMS(m, "未找到该规则", nil, 60)
				}
				return nil
			}
			infos.buildRexRules()
			sendMS(m, successMes, nil, 60)
			return nil
		default:
			if command != "" {
				return nil
			}
			if !infos.isWhite(m.SenderID()) && m.SenderID() != 0 {
				sendMS(m, "你没有使用此机器人的权限", nil, 60)
				return nil
			}
			return handleMess(m)
		}
	}
	return nil
}

// handleMess 处理接收到的普通消息，解析其中的媒体文件或 Telegram 链接
func handleMess(m *telegram.NewMessage) error {
	// 如果是用户发送或转发来的、带有图片/文档/视频的消息，直接生成直链
	if m.IsMedia() && (m.Photo() != nil || m.Document() != nil || m.Video() != nil) {
		conf := infos.Conf.Load()
		// 开启密码保护时链接需要携带发送者的 hash 鉴权,
		// 频道帖子没有个人发送者(SenderID 为 0), 无法生成有效直链;
		// 不能回退用管理员 UID, 否则等于把管理员 ID 泄露给链接接收方
		if conf.Password != "" && m.SenderID() == 0 {
			sendMS(m, "频道帖子没有个人发送者, 无法生成带鉴权的直链", nil, 60)
			return nil
		}
		params := StreamLinkParams{
			CID:  m.ChatID(),
			MID:  m.ID,
			Cate: "bot",
		}
		if m.Channel != nil && m.Channel.Username != "" {
			params.CName = m.Channel.Username
		}
		if conf.Password != "" {
			params.Hash = infos.calculateHash(m.SenderID())
		}
		link, err := buildStreamLink(conf.Site, params)
		if err != nil {
			return err
		}
		links := []string{link}
		return sendLink(m, links)
	}

	if infos.Status.Load() != 3 {
		return nil
	}

	src := strings.TrimSpace(m.Text())
	if src == "" {
		return nil
	}

	// 匹配格式如：t.me/c/12345/678 或 t.me/username/678
	matches := findTelegramLinks(src)

	if len(matches) == 0 {
		return nil
	}
	res := HackLink{
		M: m,
	}
	var items []Item
	var errs error
	for _, match := range matches {
		res.Match = match
		result, err := hackLinks(res)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		items = append(items, result...)
	}

	// 全部失败时单独告知原因; 部分成功时错误已单独回复, 成功的链接仍继续下发,
	// 不能因为存在个别失败就把全部成功结果一并丢弃
	if errs != nil {
		if _, err := m.Reply(errs.Error()); err != nil {
			log.Printf("发送消息失败: %+v", err)
		}
	}
	if len(items) == 0 {
		return nil
	}
	links := make([]string, 0, len(items))
	for _, item := range items {
		link, err := handleLinks(res, item)
		if err != nil {
			log.Printf("生成直链失败: cid=%d, mid=%d, err=%+v", item.CID, item.MID, err)
			return err
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return nil
	}
	if err := sendLink(m, links); err != nil {
		log.Printf("发送消息失败: %+v", err)
	}
	return nil
}

// sendLink 发送美化后的下载链接消息
func sendLink(m *telegram.NewMessage, links []string) error {
	return sendLinkReplies(links, func(text string, options *telegram.SendOptions) error {
		_, err := m.Reply(text, options)
		return err
	})
}

func sendLinkReplies(links []string, reply func(string, *telegram.SendOptions) error) error {
	if len(links) == 0 {
		return nil
	}

	if len(links) == 1 {
		link := links[0]
		text := fmt.Sprintf("<b>🔗 链接提取成功</b>\n\n<code>%s</code>\n\n👆 <i>上方链接复制, 下方按钮下载</i> 👇", html.EscapeString(link))
		if len(text) > maxReplyHTMLBytes {
			return errors.New("单个链接过长，无法放入 Telegram 消息")
		}
		markup := telegram.NewKeyboard().AddRow(
			telegram.Button.URL("🚀 直接下载", fmt.Sprintf("%s&download=true", link)),
		).Build()

		err := reply(text, &telegram.SendOptions{
			ParseMode:   "html",
			ReplyMarkup: markup,
		})

		if err != nil {
			log.Printf("发送下载链接失败: %+v", err)
		}
		return err
	}

	pages, err := linkReplyPages(links)
	if err != nil {
		return err
	}
	for _, page := range pages {
		if err := reply(page, &telegram.SendOptions{ParseMode: "html"}); err != nil {
			log.Printf("发送合并下载链接失败: %+v", err)
			return err
		}
	}
	return nil
}

// sendMS 统一发送消息，支持回复或主动推送给管理员，可设置自动删除延时
func sendMS(m *telegram.NewMessage, src any, params *telegram.SendOptions, wait ...int) {
	botClient := infos.BotClient.Load()
	switch {
	case m != nil:
		ms, err := m.Reply(src, params)
		if err != nil {
			log.Printf("发送消息失败: %+v", err)
		}
		if len(wait) > 0 && wait[0] > 0 && ms != nil {
			safeGo(func() {
				time.Sleep(time.Duration(wait[0]) * time.Second)
				if _, err = ms.Delete(); err != nil {
					log.Printf("删除消息失败: %+v", err)
				}
			})
		}
		return
	case botClient != nil:
		ms, err := botClient.SendMessage(infos.Conf.Load().UserID, src, params)
		if err != nil {
			log.Printf("发送消息失败: %+v", err)
		}
		if len(wait) > 0 && wait[0] > 0 && ms != nil {
			safeGo(func() {
				time.Sleep(time.Duration(wait[0]) * time.Second)
				if _, err = ms.Delete(); err != nil {
					log.Printf("删除消息失败: %+v", err)
				}
			})
		}
		return
	}
}
