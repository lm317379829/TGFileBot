package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xAmarnath/gogram/telegram"
)

// startBot 创建并连接 Bot 客户端, 注册消息处理器并设置命令菜单
func (infos *Infos) startBot() (err error) {
	conf := infos.Conf.Load()
	botID := strconv.FormatInt(infos.BotID, 10)
	if botID != "" && botID != "0" {
		cleanFiles(CleanRealm{ID: botID, Cate: "bot", Realm: "cache", Filter: true})
	}

	// 创建 Bot 客户端
	client, err := telegram.NewClient(botConf("bot"))
	if err != nil {
		// 清理缓存
		cleanFiles(CleanRealm{Cate: "bot", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "bot", Realm: "cache", Filter: false})
		log.Printf("创建 Bot 客户端失败: %+v", err)
		return err
	}
	published := false
	defer releaseClient(client.Disconnect, &published)

	// 连接 Bot
	if err = client.Connect(); err != nil {
		// 清理缓存
		cleanFiles(CleanRealm{Cate: "bot", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "bot", Realm: "cache", Filter: false})
		log.Printf("Bot 连接失败: %+v", err)
		return err
	}

	// 登录 Bot
	if err = client.LoginBot(conf.BotToken); err != nil {
		// 清理缓存
		cleanFiles(CleanRealm{Cate: "bot", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "bot", Realm: "cache", Filter: false})
		log.Printf("Bot 登录失败: %+v", err)
		return err
	}

	// 注册 Bot 命令处理函数
	client.On(telegram.OnMessage, handleBotCommand)

	// 启动一个协程并发设置 Bot 的命令列表, 不影响主流程
	safeGo(func() {
		// 先清空默认的命令列表, 确保没有权限的用户什么也看不到
		_, err := client.SetBotCommands([]*telegram.BotCommand{}, botCommandScope(nil), "")
		if err != nil {
			log.Printf("清空默认命令失败: %+v", err)
		}

		userID, err := client.ResolvePeer(conf.UserID)
		if err != nil {
			log.Printf("解析用户 ID 失败: %v", err)
			return
		}
		commands := []*telegram.BotCommand{
			{
				Command:     "qr",
				Description: "获取登录二维码",
			},
			{
				Command:     "phone",
				Description: "输入手机号登录",
			},
			{
				Command:     "code",
				Description: "输入验证码登录(需混入非数字字符)",
			},
			{
				Command:     "pass",
				Description: "输入2FA密码登录",
			},
		}
		commonCommands := []*telegram.BotCommand{
			{
				Command:     "dc",
				Description: "设置客户端默认DC",
			},
			{
				Command:     "allow",
				Description: "添加白名单",
			},
			{
				Command:     "disallow",
				Description: "移除白名单",
			},
			{
				Command:     "add",
				Description: "添加搜索频道",
			},
			{
				Command:     "del",
				Description: "移除搜索频道",
			},
			{
				Command:     "addrule",
				Description: "添加关键词规则",
			},
			{
				Command:     "delrule",
				Description: "移除关键词规则",
			},
			{
				Command:     "list",
				Description: "列出搜索频道、白名单、关键词规则",
			},
			{
				Command:     "info",
				Description: "获取程序运行信息",
			},
			{
				Command:     "size",
				Description: "设置程序缓存大小",
			},
			{
				Command:     "site",
				Description: "设置反代域名",
			},
			{
				Command:     "port",
				Description: "设置HTTP服务端口",
			},
			{
				Command:     "proxy",
				Description: "设置代理",
			},
			{
				Command:     "check",
				Description: "查找HASH对应的用户信息",
			},
			{
				Command:     "workers",
				Description: "设置并发数",
			},
			{
				Command:     "channel",
				Description: "设置绑定频道",
			},
			{
				Command:     "password",
				Description: "设置接口访问密码",
			},
		}
		commands = append(commands, commonCommands...)

		_, err = client.SetBotCommands(commands, botCommandScope(&userID), "")
		if err != nil {
			log.Printf("设置 Bot 超级管理员命令失败: %+v", err)
			return
		}

		for _, adminID := range conf.AdminIDs {
			if adminID == conf.UserID {
				continue
			}
			userID, err := client.ResolvePeer(adminID)
			if err != nil {
				log.Printf("解析用户 ID 失败: %+v", err)
				continue
			}
			_, err = client.SetBotCommands(commonCommands, botCommandScope(&userID), "")
			if err != nil {
				log.Printf("设置 Bot 管理员命令失败: %+v", err)
				continue
			}
		}
	})

	if conf.DeBUG {
		log.Printf("Bot 启动成功")
	}

	infos.BotClient.Store(client)
	published = true
	return nil
}

func releaseClient(disconnect func() error, published *bool) {
	if !*published {
		_ = disconnect()
	}
}

// userBotClient 创建并连接 UserBot 客户端（不执行登录, 仅建立连接）
func (infos *Infos) userBotClient() (err error) {
	appConf := infos.Conf.Load()
	// 清理缓存
	userID := strconv.FormatInt(appConf.UserID, 10)
	if userID != "" && userID != "0" {
		cleanFiles(CleanRealm{ID: userID, Cate: "user", Realm: "cache", Filter: true})
	}

	clientConf := botConf("user")
	if appConf.DC != 0 {
		clientConf.DataCenter = appConf.DC
	}

	client, err := telegram.NewClient(clientConf)
	if err != nil {
		// 清理缓存
		cleanFiles(CleanRealm{Cate: "user", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "user", Realm: "cache", Filter: false})
		log.Printf("创建 UserBot 客户端失败: %+v", err)
		return
	}

	// 连接 UserBot
	if err = client.Connect(); err != nil {
		// 清理缓存
		cleanFiles(CleanRealm{Cate: "user", Realm: "session"})
		cleanFiles(CleanRealm{Cate: "user", Realm: "cache", Filter: false})
		log.Printf("UserBot 连接失败: %+v", err)
		return
	}

	infos.UserClient.Store(client)

	return err
}

// startUserBot 发起手机号登录流程
func (infos *Infos) startUserBot(phone string) (err error) {
	// 用独立的 LoginMu 序列化整个登录流程: Status 在回调触发前仍是 0,
	// 仅靠状态检查无法拦截窗口期内并发到达的第二条 /phone, 会同时发起两个 Login
	if !infos.LoMu.TryLock() {
		err = errors.New("已有登录流程正在进行")
		log.Printf("UserBot 登录失败: %+v", err)
		return err
	}

	switch infos.Status.Load() {
	case 1, 2:
		// 正在进行验证码或密码输入状态, 不允许重复发起
		infos.LoMu.Unlock()
		err = errors.New("已有登录流程正在进行")
		log.Printf("UserBot 登录失败: %+v", err)
		return err
	case 3:
		// 已登录状态, 若客户端实例丢失则尝试重建
		infos.LoMu.Unlock()
		if infos.UserClient.Load() == nil {
			if err := infos.userBotClient(); err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				infos.resetStatus()
				return err
			}
		}
		return nil
	default:
		// 未登录状态, 开始新的登录流程
		if infos.UserClient.Load() == nil {
			if err := infos.userBotClient(); err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				infos.resetStatus()
				infos.LoMu.Unlock()
				return err
			}
		}
		sendMS(nil, fmt.Sprintf("收到手机号 %s, 正在尝试发送验证码...", phone), nil, 60)

		// 在协程中执行阻塞的登录命令; LoginMu 的所有权移交给该协程,
		// 直到登录成功或失败后才释放, 期间新的登录请求会被 TryLock 拒绝
		safeGo(func() {
			defer infos.LoMu.Unlock()
			status, err := infos.UserClient.Load().Login(phone, &telegram.LoginOptions{
				CodeCallback:     infos.code, // 指定验证码回调函数
				PasswordCallback: infos.pass, // 指定二步验证回调函数
				MaxRetries:       3,
			})
			if err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				sendMS(nil, fmt.Sprintf("UserBot 登录失败: %+v", err), nil, 60)
				infos.resetStatus()
				return
			}

			if status {
				if infos.Conf.Load().DeBUG {
					log.Printf("UserBot 登录成功")
				}
				if err := infos.checkStatus(); err != nil {
					log.Printf("UserBot 登录失败: %+v", err)
					infos.resetStatus()
					return
				}
			}
		})
	}
	return nil
}

const timeoutQR = 30 * time.Second

func (infos *Infos) selectionsQR() telegram.QrOptions {
	return telegram.QrOptions{
		PasswordCallback: infos.pass,
		Timeout:          int32(timeoutQR / time.Second),
	}
}

func noteQR() string {
	return fmt.Sprintf("请使用手机 Telegram 扫描此二维码登录。二维码有效期 %d 秒, 如失效请重新发送 /qr", int(timeoutQR/time.Second))
}

func mesQRTTL() int {
	return int(timeoutQR/time.Second) + 5
}

// startUserBotQR 发起二维码登录流程
func (infos *Infos) startUserBotQR() (err error) {
	// 与 startUserBot 共用 LoginMu 序列化登录流程, 防止 /qr 与 /phone 同时发起
	if !infos.LoMu.TryLock() {
		err = errors.New("已有登录流程正在进行")
		log.Printf("UserBot 登录失败: %+v", err)
		return err
	}

	switch infos.Status.Load() {
	case 1, 2:
		infos.LoMu.Unlock()
		err = errors.New("已有登录流程正在进行")
		log.Printf("UserBot 登录失败: %+v", err)
		return err
	case 3:
		infos.LoMu.Unlock()
		if infos.UserClient.Load() == nil {
			if err := infos.userBotClient(); err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				infos.resetStatus()
				return err
			}
		}
		return nil
	default:
		infos.Status.Store(1)
		if infos.UserClient.Load() == nil {
			if err := infos.userBotClient(); err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				infos.resetStatus()
				infos.LoMu.Unlock()
				return err
			}
		}
		sendMS(nil, "正在请求登录二维码...", nil, 60)

		// 启动登录流程（会阻塞, 直到登录完成或失败）; LoginMu 所有权移交给该协程
		safeGo(func() {
			defer infos.LoMu.Unlock()

			qr, err := infos.UserClient.Load().QRLogin(infos.selectionsQR())
			if err != nil {
				log.Printf("获取 QR 登录失败: %+v", err)
				// 账号开启 2FA 时 exportLoginToken 直接报 SESSION_PASSWORD_NEEDED,
				// 此时 QRLogin 返回的 qr 为 nil, 继续导出 PNG 会空指针 panic;
				// 必须终止二维码流程并引导用户改走支持密码回调的手机号登录
				if telegram.MatchError(err, "SESSION_PASSWORD_NEEDED]") {
					sendMS(nil, "账号已开启两步验证, 无法使用二维码登录, 请发送 /phone + 手机号登录", nil, 120)
				} else {
					sendMS(nil, fmt.Sprintf("获取 QR 登录失败: %+v", err), nil, 60)
				}
				infos.resetStatus()
				return
			}

			if qr.Url() == "" {
				if err := infos.checkStatus(); err != nil {
					log.Printf("UserBot 登录失败: %+v", err)
					infos.resetStatus()
				}
				return
			}

			png, err := qr.ExportAsPng()
			if err != nil {
				log.Printf("导出 QR PNG 失败: %+v", err)
				if infos.finishQR(err) {
					return
				}
			}

			src, err := infos.BotClient.Load().UploadFile(png, &telegram.UploadOptions{
				FileName: "qr.png",
			})
			if err != nil {
				log.Printf("上传 QR 文件失败: %+v", err)
				if infos.finishQR(err) {
					return
				}
			}
			sendMS(nil, src, &telegram.SendOptions{Caption: noteQR()}, mesQRTTL())
			if infos.handleQRWaitResult(qr.WaitLogin(), func(err error) {
				sendMS(nil, fmt.Sprintf("QR 登录失败: %+v", err), nil, 60)
			}) {
				return
			}

			if err := infos.checkStatus(); err != nil {
				log.Printf("UserBot 登录失败: %+v", err)
				infos.resetStatus()
				return
			}
		})
	}

	return nil
}

// checkStatus 获取当前 UserBot 登录状态并校验 ID 是否合法
func (infos *Infos) checkStatus() (err error) {
	// 登录成功
	me, getMeErr := infos.UserClient.Load().GetMe()
	if err = checkUser(me, getMeErr, infos.Conf.Load().UserID); err != nil {
		log.Printf("获取用户信息失败: %+v", err)
		infos.Mutex.Lock()
		infos.Status.Store(0)
		infos.Mutex.Unlock()
		if getMeErr != nil || me == nil {
			return err
		}
		log.Printf("登录失败: 用户ID不匹配, 期望 %d, 实际 %d", infos.Conf.Load().UserID, me.ID)
		if client := infos.UserClient.Load(); client != nil {
			if disconnectErr := client.Disconnect(); disconnectErr != nil {
				log.Printf("UserBot 退出失败: %+v", disconnectErr)
			}
		}
		infos.resetStatus()
		return combineErrors(err, infos.userBotClient())
	}

	name := me.FirstName + me.LastName
	if me.Username != "" {
		name = "@" + me.Username
	}
	src := fmt.Sprintf("登录成功! 用户: %s", name)
	log.Print(src)
	sendMS(nil, src, nil)
	infos.Mutex.Lock()
	infos.Status.Store(3)
	infos.Mutex.Unlock()
	return nil
}

func checkUser(me *telegram.UserObj, err error, expectedID int64) error {
	if err != nil {
		return fmt.Errorf("获取用户信息: %w", err)
	}
	if me == nil {
		return errors.New("获取用户信息为空")
	}
	if me.ID != expectedID {
		return fmt.Errorf("用户ID不匹配, 期望 %d, 实际 %d", expectedID, me.ID)
	}
	return nil
}

func combineErrors(validationErr, rebuildErr error) error {
	if rebuildErr == nil {
		return validationErr
	}
	return errors.Join(validationErr, rebuildErr)
}

func (infos *Infos) finishQR(err error) bool {
	if err == nil {
		return false
	}
	infos.resetStatus()
	return true
}

func (infos *Infos) handleQRWaitResult(err error, report func(error)) bool {
	if err == nil || strings.Contains(err.Error(), "scanning again") {
		return false
	}
	report(err)
	return infos.finishQR(err)
}

// resetStatus 断开 UserBot 连接并清理 session/cache, 将状态重置为未登录
func (infos *Infos) resetStatus() {
	// 排空可能残留的旧验证码/密码
	select {
	case <-infos.Code:
	default:
	}
	select {
	case <-infos.Pass:
	default:
	}

	// 1. 断开连接并清理句柄
	if client := infos.UserClient.Load(); client != nil {
		if err := client.Disconnect(); err != nil {
			log.Printf("UserBot 断开连接失败: %+v", err)
		}
	}
	// 2. 清理磁盘上的 Session 和 Cache 文件（防止因文件损坏导致的下次循环失败）
	cleanFiles(CleanRealm{Cate: "user", Realm: "session"})
	cleanFiles(CleanRealm{Cate: "user", Realm: "cache", Filter: false})

	// 3. 重置内存状态
	infos.UserClient.Store(nil)
	infos.Status.Store(0)
}

// waitSrc 是登录流程的通用等待器: CAS 原子转移状态成功后, 通过 Bot 提示用户输入,
// 再在通道与超时之间二选一等待结果, 超时自动重置为未登录状态并返回错误
func (infos *Infos) waitSrc(from, to int32, input chan string, stateName, waitMsg, timeoutMsg string) (string, error) {
	if !infos.Status.CompareAndSwap(from, to) {
		err := fmt.Errorf("当前状态不是等待%s", stateName)
		sendMS(nil, err.Error(), nil, 60)
		return "", err
	}
	timeout := time.NewTimer(2 * time.Minute)
	defer timeout.Stop()

	sendMS(nil, waitMsg, nil, 120)
	select {
	case str := <-input:
		return str, nil
	case <-timeout.C:
		err := errors.New(timeoutMsg)
		sendMS(nil, err.Error(), nil, 60)
		infos.Status.Store(0) // 流程失败, 重置为未登录状态
		return "", err
	}
}

// code 是登录回调, 暂停协程等待用户通过 Bot 发送验证码
func (infos *Infos) code() (string, error) {
	return infos.waitSrc(0, 1, infos.Code, "验证码", "等待用户输入 /code 验证码...", "等待验证码超时")
}

// submitCode 接收用户通过 Bot 发送的验证码并写入通道
func (infos *Infos) submitCode(str string) (err error) {
	infos.Mutex.Lock()

	if infos.Status.Load() != 1 {
		infos.Mutex.Unlock()
		err = errors.New("当前状态不是等待验证码")
		return err
	}

	// 过滤非数字字符
	var sb strings.Builder
	for _, r := range str {
		if isNumber(r) {
			sb.WriteRune(r)
		}
	}

	code := sb.String()
	infos.Mutex.Unlock() // 发送前解锁，允许阻塞但不会死锁全局

	// 非阻塞写入：缓冲容量为 1 且读取方 code() 是唯一消费者,
	// 缓冲满即代表已有未消费的验证码（重复提交/读取方已超时）, 直接拒绝,
	// 避免在无读取者的竞态下长时间占用 Bot 消息处理 goroutine,
	// 也不再用超时分支无条件重置 Status 而误伤已成功的登录流程
	select {
	case infos.Code <- code:
		return nil
	default:
		return errors.New("已有验证码等待处理, 请勿重复提交")
	}
}

// pass 是登录回调, 暂停协程等待用户通过 Bot 发送 2FA 密码
func (infos *Infos) pass() (string, error) {
	return infos.waitSrc(1, 2, infos.Pass, "2FA密码", "等待用户输入 /pass 2FA密码...", "等待2FA密码超时")
}

// submitPass 接收用户通过 Bot 发送的 2FA 密码并写入通道
func (infos *Infos) submitPass(pass string) (err error) {
	infos.Mutex.Lock()

	if infos.Status.Load() != 2 {
		infos.Mutex.Unlock()
		err = errors.New("当前状态不是等待2FA密码")
		return err
	}
	infos.Mutex.Unlock() // 发送前解锁，允许阻塞但不会死锁全局

	// 非阻塞写入：缓冲容量为 1 且读取方 pass() 是唯一消费者,
	// 缓冲满即代表已有未消费的密码（重复提交/读取方已超时）, 直接拒绝,
	// 避免在无读取者的竞态下长时间占用 Bot 消息处理 goroutine,
	// 也不再用超时分支无条件重置 Status 而误伤已成功的登录流程
	select {
	case infos.Pass <- pass:
		return nil
	default:
		return errors.New("已有2FA密码等待处理, 请勿重复提交")
	}
}

// cateClient 根据消息缓存记录的 cate ("user"/"bot") 解析出对应的客户端实例。
// 供 HTTP 处理器在拿到 handleMs 的结果后使用，取代此前直接读取共享字段 infos.Client 的做法。
func (infos *Infos) cateClient(cate string) *telegram.Client {
	if cate == "user" {
		return infos.UserClient.Load()
	}
	return infos.BotClient.Load()
}

// wakeTCP 预热连接，防止冷启动卡死
// client 由调用方显式传入（而非读取共享的 infos.Client），避免并发请求下客户端选择互相覆盖
func (infos *Infos) wakeTCP(client *telegram.Client, cate string) error {
	if client == nil {
		return errors.New("client 不能为 nil")
	}
	return infos.wakeTCPClient(client, cate)
}

type connectClient interface {
	PingContext(context.Context) (time.Duration, error)
	Disconnect() error
	Connect() error
}

func (infos *Infos) wakeTCPClient(client connectClient, cate string) error {
	mu := infos.connectMu(cate)
	mu.Lock()
	defer mu.Unlock()

	debug := infos.Conf.Load().DeBUG
	stat := infos.connectStat(cate)

	// 设置较短超时
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 最轻量探活 RPC
	latenc, err := client.PingContext(ctx)
	if err != nil {
		if debug {
			log.Printf("TCP 链路异常, 正在重连: %+v", err)
		}
		// 重连带退避重试, 避免一次瞬时故障就把链路误判为永久死亡
		const maxReconnect = 3
		var lastErr error
		for count := 1; count <= maxReconnect; count++ {
			// 强制断开
			if err := client.Disconnect(); err != nil {
				log.Printf("强制断开 TCP 连接失败: %+v", err)
			}
			// 重连
			if err := client.Connect(); err != nil {
				lastErr = err
				log.Printf("重连 TCP 失败(第 %d/%d 次): %+v", count, maxReconnect, err)
				time.Sleep(time.Duration(count) * 500 * time.Millisecond)
				continue
			}
			// 重连后再次验证，必须使用全新的 context，防止使用已过期的旧 context
			newCtx, newCancel := context.WithTimeout(context.Background(), 15*time.Second)
			value, verr := client.PingContext(newCtx)
			newCancel()
			if verr != nil {
				lastErr = verr
				log.Printf("重连 TCP 后验证失败(第 %d/%d 次): %+v", count, maxReconnect, verr)
				time.Sleep(time.Duration(count) * 500 * time.Millisecond)
				continue
			}
			if debug {
				log.Printf("TCP 链路已恢复, 延迟: %dms", value.Milliseconds())
			}
			stat.wake(value.Milliseconds())
			return nil
		}
		// 多次重试均失败, 最终标记链路死亡
		log.Printf("重连 TCP 连续 %d 次失败, 标记链路死亡: %+v", maxReconnect, lastErr)
		stat.markDead()
		return lastErr
	}

	if debug {
		log.Printf("TCP 链路正常, 延迟: %dms", latenc.Milliseconds())
	}
	stat.wake(latenc.Milliseconds())
	return nil
}

// botConf 构造 Telegram 客户端所需的通用配置
func botConf(cate string) (conf telegram.ClientConfig) {
	appConf := infos.Conf.Load()
	conf = telegram.ClientConfig{
		AppID:        appConf.AppID,
		AppHash:      appConf.AppHash,
		LogLevel:     infoLevel(appConf.DeBUG),
		Session:      filepath.Join(infos.FilesPath, fmt.Sprintf("%s.session", cate)),
		Cache:        telegram.NewCache(filepath.Join(infos.FilesPath, fmt.Sprintf("%s.cache", cate))),
		CacheSenders: true,
		DeviceConfig: telegram.DeviceConfig{
			DeviceModel:   "Android",
			SystemVersion: "Android 14",
			AppVersion:    "10.14.3",
		},
		FloodHandlerContext: func(ctx context.Context, err error) bool {
			wait, _ := infos.handleFloodWait(err.Error())
			waitDuration := floodWaitDuration(wait)
			log.Printf("访问太过频繁, 等待 %d 秒后重试", int64(waitDuration/time.Second))
			infos.advanceWaitUntil(wait)

			timer := time.NewTimer(waitDuration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			}
		},
	}
	if appConf.Proxy != "" {
		proxy, err := telegram.ProxyFromURL(appConf.Proxy)
		if err == nil {
			conf.Proxy = proxy
		} else {
			log.Printf("代理地址解析失败: %v", err)
		}
	}
	return conf
}

// infoLevel 根据 DeBUG 配置返回 gogram 客户端日志级别, 便于排查导出授权等底层错误
func infoLevel(debug bool) telegram.LogLevel {
	if debug {
		return telegram.LogDebug
	}
	return telegram.LogError
}

// maxFloodWaitDuration includes the one-second retry margin.
const maxFloodWaitDuration = 24 * time.Hour

func floodWaitDuration(wait int) time.Duration {
	if wait < 0 {
		wait = 0
	} else if wait >= int(maxFloodWaitDuration/time.Second)-1 {
		return maxFloodWaitDuration
	}
	return time.Duration(wait+1) * time.Second
}

// handleFloodWait 解析错误文本中的 FLOOD_WAIT 等待秒数; matched 表示正则是否命中（即应视为 Flood 处理）
func (infos *Infos) handleFloodWait(errSrc string) (wait int, matched bool) {
	wait = 3
	errSrc = strings.ToUpper(errSrc)
	matches := infos.Rex.FindStringSubmatch(errSrc)
	matched = len(matches) > 1
	if matched {
		for _, match := range matches[1:] {
			if match != "" {
				maxWaitSeconds := int(maxFloodWaitDuration/time.Second) - 1
				if value, err := strconv.Atoi(match); err == nil && value <= maxWaitSeconds {
					wait = value
				} else {
					wait = maxWaitSeconds
				}
				break
			}
		}
	} else {
		log.Printf("错误信息不是 FLOOD WAIT: %s", errSrc)
	}
	return wait, matched
}

// advanceWaitUntil 只增不减地推进全局 FloodWait 截止时间, 避免后续短等待覆盖仍在生效的长等待
func (infos *Infos) advanceWaitUntil(wait int) {
	waitUntil := time.Now().Add(floodWaitDuration(wait))
	infos.advanceWaitUntilUnix(waitUntil.Unix())
}

func (infos *Infos) advanceWaitUntilUnix(candidate int64) {
	for {
		current := infos.WaitUntil.Load()
		if candidate <= current || infos.WaitUntil.CompareAndSwap(current, candidate) {
			return
		}
	}
}

// list
func (infos *Infos) list(channel string, page, limit int, offset int32, filter int64, reverse bool, ctx context.Context) (items Items, err error) {
	channelInfo, err := infos.handleChannel(channel)
	if err != nil {
		return items, err
	}
	if page == 1 {
		handleOffset("del", channel, 0)
	} else {
		offset = handleOffset("get", fmt.Sprintf("%s|%d", channel, page), 0)
		// 偏移量缓存过期（>1h）时 offset 回退为 0, 则从第 1 页重新开始, 不再报错
	}

	params := HandleMs{
		CID:      channelInfo.CID,
		OffsetID: offset,
		Limit:    limit,
		Filter:   &telegram.InputMessagesFilterPhotoVideo{},
		Ctx:      ctx,
		Cate:     "user",
	}

	msCache, err := infos.handleMs(params)
	if err != nil {
		return items, err
	}

	ms := msCache.load()
	lenMs := len(ms)

	switch lenMs {
	case 0:
		return items, errors.New("未找到匹配消息")
	case limit:
		handleOffset("set", fmt.Sprintf("%s|%d", channel, page+1), ms[lenMs-1].ID)
		items.HasMore = true
	}

	// 按频道读取上一页遗留的相册边界去重信息, latestMIDs 精确匹配消息 ID,
	// 不再用字符串子串匹配（会把 ID=12 误判为 ID=123 的子串导致误删), 且按频道隔离, 避免不同频道间 mid 相同时互相污染
	infos.Mutex.RLock()
	latestGroup := infos.LatestMIDss[channel]
	infos.Mutex.RUnlock()
	latestCount := 0
	var latestMIDs map[int32]bool
	if latestGroup != nil {
		latestCount = latestGroup.Count
		latestMIDs = latestGroup.MIDs
	}

	mids := make(map[int32]bool)
	maxNum := len(ms) - 1
	TCPDead := false
	for num, m := range ms {
		if m.File == nil {
			continue
		}
		if num <= latestCount && latestMIDs[m.ID] {
			continue
		}

		if value, ok := mids[m.ID]; ok && value {
			continue
		}

		if isVideoFile(m.File.Ext) && m.File.Size < filter {
			continue
		}

		if items.Channel == "" {
			items.Channel = strings.TrimSpace(m.Channel.Title)
		}

		if (num == 0 || num == maxNum) && m.Message.GroupedID != 0 {
			medias, err := m.GetMediaGroup()
			if err != nil {
				log.Printf("提取媒体组错误: %+v", err)
				TCPDead = true
			}

			count := 0
			newMIDs := make(map[int32]bool, len(medias))
			for _, media := range medias {
				if isVideoFile(media.File.Ext) && media.File.Size < filter {
					continue
				}
				if value, ok := mids[media.ID]; ok && value {
					continue
				}

				if latestMIDs[media.ID] {
					break
				}

				count++
				newMIDs[media.ID] = true
				mids[media.ID] = true
				item := handleItem(media)
				items.Item = append(items.Item, item)
			}
			if num == maxNum {
				infos.Mutex.Lock()
				if evictOldestLatestGID(infos.LatestMIDss, infos.MaxChannel) {
					infos.LatestMIDss[channel] = &LatestMIDs{Count: count, MIDs: newMIDs, Time: time.Now()}
				}
				infos.Mutex.Unlock()
			}
		} else {
			mids[m.ID] = true
			item := handleItem(m)
			items.Item = append(items.Item, item)
		}
	}

	sortItems(items.Item, reverse)
	items.ID = channel

	if TCPDead {
		safeGo(func() {
			status := infos.connectStat("user")
			status.fail(infos.UserClient.Load())
			if status.TCPDead.Load() {
				if err := infos.wakeTCP(infos.UserClient.Load(), "user"); err != nil {
					log.Printf("TCP 重连失败: %+v", err)
				} else if infos.Conf.Load().DeBUG {
					log.Print("TCP 重连成功")
				}
			}
		})
	}
	return items, nil
}

// search 在指定频道中搜索关键词并返回匹配的媒体文件列表
func (infos *Infos) search(channel, keywords string, page, limit int, offset int32, filter int64, reverse bool, ctx context.Context) (items Items, err error) {
	channelInfo, err := infos.handleChannel(channel)
	if err != nil {
		return items, err
	}

	if offset == 0 {
		key := fmt.Sprintf("%s|%s|%d", channel, keywords, page)
		offset = handleOffset("get", key, offset)
		// 偏移量缓存过期（>1h）时 offset 回退为 0, 则从第 1 页重新开始, 不再报错
	}

	params := HandleMs{
		CID:      channelInfo.CID,
		OffsetID: offset,
		Limit:    limit,
		Filter:   &telegram.InputMessagesFilterPhotoVideo{},
		Ctx:      ctx,
		Words:    keywords,
		Cate:     "user",
	}

	msCache, err := infos.handleMs(params)
	if err != nil {
		return items, err
	}

	ms := msCache.load()
	lenMs := len(ms)
	switch lenMs {
	case 0:
		return items, errors.New("未找到匹配消息")
	case limit:
		key := fmt.Sprintf("%s|%s|%d", channel, keywords, page+1)
		handleOffset("set", key, ms[lenMs-1].ID)
		items.HasMore = true
	}

	for _, m := range ms {
		if m.File == nil {
			continue
		}

		if isVideoFile(m.File.Ext) && m.File.Size < filter {
			continue
		}

		if items.Channel == "" {
			items.Channel = strings.TrimSpace(m.Channel.Title)
		}
		items.Item = append(items.Item, handleItem(m))
	}
	items.ID = channel
	items.Word = keywords
	sortItems(items.Item, reverse)
	return items, nil
}

// listFreshTTL 是首页列表/搜索结果的短 TTL, 保证新发布的文件能及时出现在首页,
// 同时避免每次请求都实时打 Telegram 造成的 FLOOD_WAIT 压力
const listFreshTTL = 30 * time.Second

// handleMs 根据当前网络延迟选择最佳客户端
// 返回约定: 成功时返回非 nil 的消息缓存; 失败时 err 非 nil,
// 同时 result 仍是非 nil 的空 MsCache（load 得到空切片）。
// 这样调用方即使漏检查 err, 也不会因 nil 指针 panic——见下预初始化。
func (infos *Infos) handleMs(params HandleMs) (result *MsCache, err error) {
	debug := infos.Conf.Load().DeBUG

	// 1. 选择下载客户端
	// 注意：客户端选择结果保存在局部变量 client 中，不写回共享字段，
	// 避免并发请求下 A 请求选中的客户端被 B 请求的选择覆盖（数据竞争）
	var client *telegram.Client
	if params.Cate == "user" && infos.Status.Load() == 3 {
		client = infos.UserClient.Load()
	} else {
		params.Cate = "bot"
		client = infos.BotClient.Load()
	}

	// 预初始化命名返回值 result 为非 nil 空对象: 使下方所有错误路径
	// (wakeTCP 失败 / handleChannel 失败 / GetMessages 失败 / 空结果)
	// 都返回合法的 *MsCache, 而非 nil 指针。MsCache 零值安全:
	// load() 对空对象返回 nil 切片, 不会 panic。
	result = &MsCache{Cate: params.Cate}

	stat := infos.connectStat(params.Cate)
	latenc := stat.Latenc.Load()
	duration := stat.since()

	// 2. 统一处理 TCP 链路检查与唤醒逻辑
	// 当 TCPDead 为 true 或距离上次唤醒超过 30 分钟时强制触发探活重连
	switch {
	case duration.Minutes() > 30 || stat.TCPDead.Load():
		if err = infos.wakeTCP(client, params.Cate); err != nil {
			log.Printf("唤醒 TCP 连接失败: %+v", err)
			return result, err
		}
	case debug:
		minutes := int(duration.Minutes())
		seconds := int(duration.Seconds()) % 60
		if minutes != 0 {
			timeStr := fmt.Sprintf("%02d分%02d秒", minutes, seconds)
			timeStr = strings.TrimPrefix(timeStr, "0")
			log.Printf("TCP 链路正常, %s前唤醒, 延迟: %d毫秒", timeStr, latenc)
		} else {
			log.Printf("TCP 链路正常, %d秒前唤醒, 延迟: %d毫秒", seconds, latenc)
		}
	}

	// 3. 获取消息
	if params.Limit == 0 {
		params.Limit = 100
	}

	src := ""
	kname := params.Cate

	var channelInfo struct {
		value    any
		Username string
	}
	if len(params.CNames) > 0 {
		channel, err := infos.handleChannel(params.CNames[0])
		if err != nil {
			return result, err
		}
		channelInfo.value = params.CNames[0]
		channelInfo.Username = channel.UserName
		params.CID = channel.CID
		src = "name=" + channel.UserName
		kname += ":" + channel.UserName
	} else {
		channelInfo.value = params.CID
	}

	cidStr := strconv.FormatInt(params.CID, 10)
	if src == "" {
		src = "cid=" + cidStr
	} else {
		src += ", cid=" + cidStr
	}
	kname += ":" + cidStr

	if len(params.MIDs) > 0 {
		src += ", mids=["
		for _, mid := range params.MIDs {
			midStr := strconv.FormatInt(int64(mid), 10)
			src += midStr + ", "
			kname += ":" + midStr
		}
		src = strings.TrimRight(src, ", ")
		src += "]"
	}

	if params.OffsetID > 0 {
		offsetIDStr := strconv.FormatInt(int64(params.OffsetID-1), 10)
		src += ", offset=" + offsetIDStr
		kname += ":" + offsetIDStr
	}

	if params.Words != "" {
		src += ", keywords=" + params.Words
		kname += ":" + params.Words
	}

	lenMIDs := len(params.MIDs)
	if lenMIDs > 0 && params.Limit > lenMIDs {
		params.Limit = lenMIDs
	}

	// 首页列表/搜索请求（无消息 ID、无翻页游标）属于"新鲜数据"类请求,
	// 缓存命中需接受较短的 TTL, 使新发布的文件能及时在首页出现;
	// 锚点类请求（带 MIDs/OffsetID）则保持长期命中以稳定翻页
	freshDo := lenMIDs == 0 && params.OffsetID == 0

	// 不同 Limit 的请求不能共用同一份缓存, 否则会返回条数与请求不符的结果（见 kname 说明）
	kname += ":limit=" + strconv.Itoa(params.Limit)

	hit := false
	infos.Mutex.RLock()
	result, ok := infos.MsCache[kname]
	infos.Mutex.RUnlock()

	// hit 的判断与 result.Time 的更新必须在同一把锁内完成：Time 可能被
	// refreshMs 或流式下载完成后的缓存回写并发更新（Mes 本身是原子快照, 无需锁保护读取）
	if ok {
		infos.Mutex.Lock()
		mes := result.load()
		if len(mes) > 0 {
			if freshDo {
				// 首页请求只接受短 TTL 内的结果, 过期即重新拉取, 保证新内容及时可见
				if time.Since(result.Time) < listFreshTTL {
					hit = true
					result.Time = time.Now()
				}
			} else if len(mes) >= params.Limit {
				hit = true
				result.Time = time.Now()
			}
		}
		infos.Mutex.Unlock()
	}

	if hit {
		if debug {
			log.Printf("命中消息缓存: %s", kname)
		}
	} else {
		param := &telegram.SearchOption{
			IDs:     params.MIDs,
			Query:   params.Words,
			Limit:   int32(params.Limit),
			Offset:  params.OffsetID,
			Context: params.Ctx,
			Filter:  params.Filter,
		}
		ms, err := client.GetMessages(channelInfo.value, param)
		if err != nil {
			// 异步尝试重连, 使后续请求到达时连接可能已恢复
			safeGo(func() {
				stat.fail(client)
				if err := infos.wakeTCP(client, params.Cate); err != nil {
					log.Printf("TCP 重连失败: %+v", err)
				} else if debug {
					log.Print("TCP 重连成功")
				}
			})
			return result, err
		}

		if len(ms) == 0 {
			err = errors.New("未获取到消息")
			if debug {
				log.Printf("获取消息失败: %s, count=%d, err=%+v", src, len(ms), err)
			}
			return result, err
		}
		result = &MsCache{Time: time.Now(), Cate: params.Cate, Username: channelInfo.Username}
		result.setMes(ms)
		if freshDo {
			// 首页列表/搜索: 缓存一切非空结果（含不足一页的末尾页), 结合短 TTL 命中
			if len(ms) > 0 {
				infos.Mutex.Lock()
				if evictOldestMsCache(infos.MsCache, infos.MaxMs) {
					infos.MsCache[kname] = result
				}
				infos.Mutex.Unlock()
			}
		} else if len(ms) == params.Limit {
			// 锚点请求（带 MIDs/OffsetID）仅在取满一页时缓存, 语义与修改前保持一致
			infos.Mutex.Lock()
			if evictOldestMsCache(infos.MsCache, infos.MaxMs) {
				infos.MsCache[kname] = result
			}
			infos.Mutex.Unlock()
		}
	}

	return result, nil
}

// 刷新消息, 用于异步下载完成后的缓存更新
// client 由调用方显式传入（而非读取共享的 infos.Client），避免并发请求下客户端选择互相覆盖
func (infos *Infos) refreshMs(client *telegram.Client, version int64, params HandleMs, msCache *MsCache) (src telegram.NewMessage, err error) {
	debug := infos.Conf.Load().DeBUG

	// 快速路径：版本已变化, 说明其它协程已完成刷新, 直接复用其结果。
	// Version 与 Mes 均为原子操作且写入方先存快照再递增版本, 顺序一致保证读到新版本时必能读到新快照
	if version != msCache.Version.Load() {
		cur := msCache.load()
		if len(cur) > 0 {
			src = cur[0]
			if debug {
				log.Printf("文件引用已刷新, 直接使用新版本, cid=%d, mids=%v, name=%s, version=%d, newVersion=%d", params.CID, params.MIDs, src.File.Name, version, msCache.Version.Load())
			}
			return src, nil
		}
		log.Printf("文件引用已刷新, 但未获取到消息, cid=%d, mids=%v, version=%d, newVersion=%d", params.CID, params.MIDs, version, msCache.Version.Load())
		return src, errors.New("未获取到消息")
	}

	// 网络 IO 不持有全局锁, 避免阻塞其它所有依赖 infos.Mutex 的操作
	ms, err := client.GetMessages(params.CID, &telegram.SearchOption{
		IDs:     params.MIDs,
		Context: params.Ctx,
	})
	if err != nil {
		safeGo(func() {
			status := infos.connectStat(params.Cate)
			status.fail(client)
			if status.TCPDead.Load() {
				if err := infos.wakeTCP(client, params.Cate); err != nil {
					log.Printf("TCP 重连失败: %+v", err)
				} else if infos.Conf.Load().DeBUG {
					log.Print("TCP 重连成功")
				}
			}
		})
		log.Printf("刷新文件引用失败: %+v", err)
		return src, err
	}
	if len(ms) == 0 {
		err = errors.New("未获取到消息")
		log.Printf("未获取到消息: cid=%v, mids=%v", params.CID, params.MIDs)
		return src, err
	}
	src = ms[0]
	if !src.IsMedia() {
		err = errors.New("消息不包含媒体")
		log.Printf("消息不包含媒体: cid=%v, mids=%v", params.CID, params.MIDs)
		return src, err
	}

	// 回写前再次校验版本：拉取期间其它协程可能已完成刷新/回写, 此时应丢弃本次结果, 采用更新版本
	infos.Mutex.Lock()
	defer infos.Mutex.Unlock()
	if version != msCache.Version.Load() {
		if cur := msCache.load(); len(cur) > 0 {
			src = cur[0]
			if debug {
				log.Printf("文件引用已刷新, 直接使用新版本, cid=%d, mids=%v, name=%s, version=%d, newVersion=%d", params.CID, params.MIDs, src.File.Name, version, msCache.Version.Load())
			}
		}
		return src, nil
	}
	msCache.setMes(ms)
	msCache.Time = time.Now()
	msCache.Version.Add(1)
	if debug {
		log.Printf("缓存数据更新, cid=%d, mids=%v, name=%s, version=%d", params.CID, params.MIDs, src.File.Name, msCache.Version.Load())
	}
	return src, nil
}

// handleChannel 处理频道ID, 返回 InputPeer
func (infos *Infos) handleChannel(channel string, hash ...int64) (result ChannelInfo, err error) {
	infos.Mutex.RLock()
	cache, ok := infos.ChannelID[channel]
	infos.Mutex.RUnlock()
	if !ok {
		src := strings.TrimPrefix(channel, "@")
		if isAllNumber(src) {
			if !strings.HasPrefix(src, "-100") {
				src = "-100" + src
			}
			cid, err := strconv.ParseInt(src, 10, 64)
			if err != nil {
				log.Printf("频道 %s 解析失败: %+v", channel, err)
				return result, err
			}
			result.CID = cid
			if len(hash) > 0 && hash[0] != 0 {
				result.Hash = hash[0]
			} else {
				result.Hash = 0
			}
			result.Peer = &telegram.InputPeerUser{
				UserID:     cid,
				AccessHash: result.Hash,
			}
		} else {
			client := infos.UserClient.Load()
			if client == nil {
				return result, errors.New("UserBot 客户端未就绪")
			}
			values, err := client.ResolvePeer(channel)
			if err != nil {
				safeGo(func() {
					status := infos.connectStat("user")
					status.fail(client)
					if status.TCPDead.Load() {
						if err := infos.wakeTCP(client, "user"); err != nil {
							log.Printf("TCP 重连失败: %+v", err)
						} else if infos.Conf.Load().DeBUG {
							log.Print("TCP 重连成功")
						}
					}
				})
				log.Printf("频道解析失败: %+v", err)
				return result, err
			}
			result.UserName = channel
			result.Peer = values
			switch value := values.(type) {
			case *telegram.InputPeerChannel:
				// 匹配到频道
				result.CID = value.ChannelID
				result.Hash = value.AccessHash
				result.Peer = value
			case *telegram.InputPeerUser:
				// 匹配到用户（假设有 UserID）
				result.CID = value.UserID
				result.Hash = value.AccessHash
				result.Peer = value
			case *telegram.InputPeerChat:
				// 匹配到普通群
				result.CID = value.ChatID
				if len(hash) > 0 && hash[0] != 0 {
					result.Hash = hash[0]
				} else {
					result.Hash = 0
				}
				result.Peer = value
			default:
				return result, errors.New("未知或不支持的 Peer 类型")
			}
			result.Time = time.Now()
			infos.Mutex.Lock()
			if evictOldestChannelCache(infos.ChannelID, infos.MaxChannel) {
				infos.ChannelID[channel] = &result
			}
			infos.Mutex.Unlock()
		}
	} else {
		infos.Mutex.Lock()
		cache.Time = time.Now()
		infos.Mutex.Unlock()
		result = *cache
		if infos.Conf.Load().DeBUG {
			log.Printf("命中频道缓存: %s", channel)
		}
	}
	return result, nil
}

// handleComments 处理评论消息，返回评论消息列表
// base 是调用方从 msCache.load() 取到的只读快照; 若需要追加评论, 会在内部先做一次拷贝,
// 绝不在共享快照的底层数组上 append。返回值 ms 可能与 base 同底（无评论场景, 零拷贝）,
// 也可能是追加了评论的新切片。
// limit 为本次希望拉取的评论条数（对应 HTTP 请求的分页大小), hasMore 表示 Telegram 一侧是否还有更多评论未拉取,
// 由"实际拉取到的原始评论条数是否达到 limit"判断——不能用追加到 ms 后的条数判断, 因为其中非媒体消息会被过滤掉。
// page 的续传方式对齐 search()：offset==0 时按 "comments|mid|page" 查缓存拿到真实游标，
// 不在缓存里且 page>1 说明跳页请求，直接报错（不能像 page=1 那样从头拉取）
func (infos *Infos) handleComments(mid, offset int32, page, limit int, base []telegram.NewMessage) (ms []telegram.NewMessage, hasMore bool, err error) {
	ms = base
	if len(ms) == 0 {
		return ms, false, errors.New("未找到消息")
	}
	if limit <= 0 {
		limit = 100
	}

	if offset == 0 {
		key := fmt.Sprintf("comments|%d|%d", mid, page)
		offset = handleOffset("get", key, offset)
		if page > 1 && offset == 0 {
			return ms, false, errors.New("未找到匹配消息")
		}
	}

	src := ms[0]
	if src.Message.Replies != nil && src.Message.Replies.ChannelID != 0 {
		discussionID := src.Message.Replies.ChannelID
		username := src.Channel.Username
		if username == "" {
			username = strconv.FormatInt(src.Chat.ID, 10)
		}
		channelInfo, err := infos.handleChannel(username)
		if err != nil {
			log.Printf("获取频道失败: %+v", err)
			return ms, false, err
		}
		if channelInfo.Hash == 0 && src.Channel.AccessHash != 0 {
			channelInfo.Hash = src.Channel.AccessHash
			channelInfo.Peer = &telegram.InputPeerChannel{
				ChannelID:  src.Channel.ID,
				AccessHash: channelInfo.Hash,
			}
		}
		client := infos.UserClient.Load()
		results, err := client.MessagesGetReplies(&telegram.MessagesGetRepliesParams{
			Peer:     channelInfo.Peer,
			Limit:    int32(limit),
			OffsetID: offset,
			MsgID:    mid,
		})

		if err != nil {
			safeGo(func() {
				status := infos.connectStat("user")
				status.fail(client)
				if status.TCPDead.Load() {
					if err := infos.wakeTCP(client, "user"); err != nil {
						log.Printf("TCP 重连失败: %+v", err)
					} else if infos.Conf.Load().DeBUG {
						log.Print("TCP 重连成功")
					}
				}
			})
			log.Printf("获取评论消息失败: cid=%d, mid=%d, err=%v", src.Channel.ID, mid, err)
			return ms, false, err
		}

		// 从 MessagesGetReplies 的结果中提取原始消息列表和随附的 Chats。
		// Chats 里带有讨论组的完整 AccessHash——必须在 PackMessages 之前注册进客户端缓存，
		// 否则 packMessage 内部按 PeerID 反查频道时缓存未命中，只能用 access_hash=0 现猜，
		// 导致除第一条(种子消息本身、频道信息天然已缓存)外的评论消息 Channel 解析失败/错误，
		// 使得 item.CID 缺失或不正确，播放时后端按 cid+mid 找不到对应媒体。
		var newMs []telegram.Message
		var chats []telegram.Chat
		switch v := results.(type) {
		case *telegram.MessagesMessagesSlice:
			newMs, chats = v.Messages, v.Chats
		case *telegram.MessagesChannelMessages:
			newMs, chats = v.Messages, v.Chats
		case *telegram.MessagesMessagesObj:
			newMs, chats = v.Messages, v.Chats
		default:
			log.Printf("收到未知的底层具体类型: %T, %v", v, v)
		}

		var discussionChannel *telegram.Channel
		for _, ch := range chats {
			if channel, ok := ch.(*telegram.Channel); ok {
				client.Cache.UpdateChannel(channel)
				if channel.ID == discussionID {
					discussionChannel = channel
				}
			}
		}

		// 拉到的原始评论数达到 limit, 说明 Telegram 一侧大概率还有更多未拉取的评论
		hasMore = len(newMs) >= limit

		// PackMessages 将 []telegram.Message 转为 []*telegram.NewMessage；
		// 上面已注册频道缓存，这里再兜底纠正一次 Channel(此前误写为 Chat.ID，对 item.CID 无效果)
		// base 快照是共享只读的, 追加前必须拷贝出私有副本, 避免并发写者落在同槽位上竞争
		startLen := len(ms)
		ms = append([]telegram.NewMessage(nil), ms...)
		for _, nm := range telegram.PackMessages(client, newMs) {
			if !nm.IsMedia() {
				continue
			}
			if nm.Channel == nil || nm.Channel.ID != discussionID {
				switch {
				case discussionChannel != nil:
					nm.Channel = discussionChannel
				case nm.Channel != nil:
					nm.Channel.ID = discussionID
					nm.Channel.Title = src.Channel.Title
					nm.Channel.Username = src.Channel.Username
					nm.Channel.AccessHash = src.Channel.AccessHash
				}
			}
			ms = append(ms, *nm)
		}

		// 记录下一页的续传游标, 供下次 page+1 请求时通过 handleOffset("get", ...) 取回，
		// 跟 search() 里 "page -> offset" 的续传方式保持一致
		if hasMore && len(ms) > startLen {
			key := fmt.Sprintf("comments|%d|%d", mid, page+1)
			handleOffset("set", key, ms[len(ms)-1].ID)
		}
	}
	return ms, hasMore, nil
}

// handleLinks 处理消息媒体, 返回直链
func handleLinks(res HackLink, item Item) (link string, err error) {
	conf := infos.Conf.Load()
	params := StreamLinkParams{
		CID:   item.CID,
		MID:   item.MID,
		Cate:  "user",
		CName: item.Username,
	}

	if conf.Password != "" {
		if res.M != nil {
			// 开启密码保护时链接需要携带发送者的 hash 鉴权,
			// 频道帖子没有个人发送者(SenderID 为 0), 无法生成有效直链;
			// 不能回退用管理员 UID, 否则等于把管理员 ID 泄露给链接接收方
			if res.M.SenderID() == 0 {
				return "", errors.New("消息没有个人发送者, 无法生成带鉴权的直链")
			}
			params.Hash = infos.calculateHash(res.M.SenderID())
		} else {
			switch {
			case res.Hash != "":
				params.Hash = res.Hash
			case res.Pass != "":
				params.Key = res.Pass
			default:
				log.Print("未提供密码或哈希")
			}
		}
	}
	link, err = buildStreamLink(conf.Site, params)
	if err != nil {
		return "", err
	}
	return link, nil
}

// handleItem 处理消息媒体, 返回 Item
func handleItem(m telegram.NewMessage) (item Item) {
	src := strings.TrimSpace(m.Text())
	src = strings.ReplaceAll(src, "_", "-")
	src = strings.TrimSpace(src)

	var last rune
	var srcBuilder strings.Builder
	srcBuilder.Grow(len(src))
	for _, char := range src {
		if unicode.IsSpace(char) && char == last {
			continue
		}
		srcBuilder.WriteRune(char)
		last = char
	}
	src = srcBuilder.String()

	name := strings.TrimSpace(m.File.Name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.Join(strings.Fields(name), " ")

	item.Ext = m.File.Ext
	item.Src = src
	item.Name = name
	item.Size = m.File.Size
	item.CID = m.Channel.ID
	item.Username = m.Channel.Username
	item.MID = m.ID
	if m.Message != nil {
		item.Date = m.Message.Date
		item.GID = m.Message.GroupedID
	}
	return item
}
