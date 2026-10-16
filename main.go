package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
)

type Params struct {
	CID      int64
	UID      int64
	Offset   int32
	MID      int32
	Page     int
	Limit    int
	Reverse  bool
	Pass     string
	Cate     string
	Hash     string
	Link     string
	Keywords string
	Filters  []int64
	Channels []string
}

type StreamLinkParams struct {
	CID   int64
	MID   int32
	Cate  string
	CName string
	Hash  string
	Key   string
}

func buildStreamLink(site string, params StreamLinkParams) (string, error) {
	parsed, err := url.Parse(site)
	if err != nil {
		return "", fmt.Errorf("解析站点地址: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("站点地址必须是包含主机名的绝对 HTTP(S) URL: %q", site)
	}

	escapedPath := strings.TrimRight(parsed.EscapedPath(), "/") + "/stream"
	path, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", fmt.Errorf("解析站点路径: %w", err)
	}
	parsed.Path = path
	parsed.RawPath = escapedPath
	parsed.Fragment = ""
	parsed.RawFragment = ""

	query := parsed.Query()
	query.Set("cid", strconv.FormatInt(params.CID, 10))
	query.Set("mid", strconv.FormatInt(int64(params.MID), 10))
	query.Set("cate", params.Cate)
	query.Set("cname", params.CName)
	query.Set("hash", params.Hash)
	query.Set("key", params.Key)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type ChannelInfo struct {
	CID      int64
	Hash     int64
	UserName string
	Peer     telegram.InputPeer
	Time     time.Time
}

// LatestMIDs 记录某个频道最近一次跨页相册（媒体组）边界的去重信息，
// 避免翻页时把上一页已经展示过的相册子项重复展示一遍。
// 按频道分别保存（而非共用一份全局状态），因为不同频道的消息 ID 完全可能重合。
type LatestMIDs struct {
	Count int            // 上一次为该边界相册实际展示的子项数量, 新一页只需检查前 Count 条消息即可
	MIDs  map[int32]bool // 上一次已经展示过的相册子项消息 ID 集合（精确匹配, 不使用子串匹配）
	Time  time.Time      // 最近一次更新时间, 供淘汰策略使用
}

// HackLink 结构体用于在处理提取链接时传递中间数据
type HackLink struct {
	M      *telegram.NewMessage // 原始消息对象
	Ctx    context.Context      // 上下文
	Offset int32                // 偏移量
	UID    int64                // 发起请求的用户 ID
	Pass   string               // 可选密码
	Hash   string               // 验证哈希
	Match  []string             // 单条 t.me 链接的正则匹配结果（teleLinkRe 的一个 submatch）
}

type HandleMs struct {
	CID      int64
	OffsetID int32
	Limit    int
	MIDs     []int32
	Cate     string
	Words    string
	CNames   []string
	Ctx      context.Context
	Filter   telegram.MessagesFilter
}

// CleanRealm 结构体用于定义清理缓存和会话的范围
type CleanRealm struct {
	Filter bool   // 是否启用过滤, 只删除特定 ID 以外的文件
	ID     string // 过滤 ID（如账号 ID）
	Cate   string // 类型：bot 或 user
	Realm  string // 范围：cache 或 session
}

type OffSet struct {
	Offset int32     // 偏移量
	Time   time.Time // 时间
}

type OffSets struct {
	Mutex   *sync.Mutex       // 互斥锁, 保护并发安全
	OffSets map[string]OffSet // 偏移量映射
}

type MediaContent struct {
	Start   int64
	End     int64
	Content []byte
}

type MediaCache struct {
	Contents []MediaContent
	Time     time.Time
}

type MsCache struct {
	Mes      atomic.Pointer[[]telegram.NewMessage] // 不可变消息快照, 更新时整体替换指针（写时拷贝）, 读者无锁直读
	Username string
	Cate     string
	Time     time.Time
	Version  atomic.Int64
}

// load 返回当前消息快照。快照一经发布就不会再被原地修改——所有写入方都在持有
// infos.Mutex 的情况下用 setMes 整体替换指针, 而 atomic 的加载/存储是顺序一致的,
// 因此读者无需加锁即可拿到完整的新快照或完整的旧快照。
// 返回的切片与缓存共享底层数组且视为只读: 调用方不得对其 append 或修改元素
// （append 可能写入共享数组的空闲槽位, 与其他并发读者竞争）; 需要变更时先自行拷贝,
// 见 handleComments（唯一需要追加评论消息的场景）
func (msCache *MsCache) load() []telegram.NewMessage {
	if ms := msCache.Mes.Load(); ms != nil {
		return *ms
	}
	return nil
}

// setMes 以写时拷贝的方式替换整个消息快照。调用方必须已持有 infos.Mutex,
// 保证同一缓存的 Time/Version 等字段的更新互斥
func (msCache *MsCache) setMes(ms []telegram.NewMessage) {
	msCache.Mes.Store(&ms)
}

type Item struct {
	Ext      string `json:"ext"`
	Src      string `json:"src"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Date     int32  `json:"date"`
	MID      int32  `json:"mid"`
	CID      int64  `json:"cid"`
	GID      int64  `json:"gid"`
	Size     int64  `json:"size"`
}

type Items struct {
	HasMore bool   `json:"more"`
	ID      string `json:"id"`
	Word    string `json:"word"`
	Channel string `json:"channel"`
	Item    []Item `json:"item"`
}

type ID struct {
	IsAdmin bool
	IsWhite bool
}

// TCPStatu 记录一路 TCP 连接的探活状态, 字段用原子类型是因为会被多个并发 HTTP 请求 goroutine 同时读写
type TCPStatu struct {
	Latenc   atomic.Int64 // 延迟, 单位毫秒
	WakeTime atomic.Int64 // 最近一次探活/唤醒时间, UnixNano; 零值表示"从未探活", 会强制触发下一次 wakeTCP
	TCPDead  atomic.Bool  // TCP 是否断开
}

// wake 记录一次成功的探活/唤醒, 更新延迟和唤醒时间, 标记 TCP 为正常
func (s *TCPStatu) wake(latenc int64) {
	s.Latenc.Store(latenc)
	s.WakeTime.Store(time.Now().UnixNano())
	s.TCPDead.Store(false)
}

// touch 刷新唤醒时间并标记 TCP 为正常
// 仅当 TCP 正常时才调用，此时连接已恢复，可以安全重置
func (s *TCPStatu) touch() {
	s.WakeTime.Store(time.Now().UnixNano())
	s.TCPDead.Store(false)
}

func (s *TCPStatu) markDead() {
	s.TCPDead.Store(true)
	s.WakeTime.Store(0)
}

// fail 标记 TCP 断开并清空唤醒时间, 使下一个请求强制走 wakeTCP 重连探活
func (s *TCPStatu) fail(client *telegram.Client) {
	if client != nil {
		if !client.IsConnected() {
			s.markDead()
		}
	} else {
		s.markDead()
	}
}

// since 返回距离上次探活/唤醒过去的时长
func (s *TCPStatu) since() time.Duration {
	return time.Since(time.Unix(0, s.WakeTime.Load()))
}

// Infos 结构体保存了程序运行时的全局状态和资源句柄
type Infos struct {
	BotClient   atomic.Pointer[telegram.Client] // 独立的 Bot 客户端（用于与用户交互）, 原子指针支持无锁并发读写
	UserClient  atomic.Pointer[telegram.Client] // 全局 UserBot 客户端实例（用于读取私有内容和流式传输）, 原子指针支持无锁并发读写
	Mutex       *sync.RWMutex                   // 全局互斥锁, 保护并发安全
	Cond        *sync.Cond                      // 条件变量, 用于搜索并发限流等待（独立锁, 不与 Mutex 共用）
	Conf        atomic.Pointer[Conf]            // 全局配置快照, 原子指针支持无锁并发读；更新走 refreshConf（写时拷贝）
	ConfMu      *sync.Mutex                     // 序列化配置更新, 避免并发管理员命令互相覆盖对方的修改
	LoMu        *sync.Mutex                     // 序列化 UserBot 登录流程, 覆盖从发起登录到后台 Login 结束的完整窗口, 防止并发 /phone、/qr 同时发起多个 Login
	File        *os.File                        // 日志文件句柄
	Rex         *regexp.Regexp                  // 用于解析 Telegram FloodWait 错误的正则
	RexRules    []*regexp.Regexp                // 预编译的群管正则规则缓存
	FilesPath   string                          // 配置文件存放目录
	FilePath    string                          // 日志文件路径
	MaxMs       int                             // 最大消息数
	MaxChannel  int                             // 最大频道数
	MaxMedia    int                             // 最大媒体数
	BotID       int64                           // Bot 自身的 ID
	Status      atomic.Int32                    // UserBot 登录状态: 0 未登录, 1 等待验证码, 2 等待二步验证, 3 已登录
	WaitUntil   atomic.Int64                    // 等待结束时间
	Code        chan string                     // 用于接收异步提交的验证码
	Pass        chan string                     // 用于接收异步提交的二步验证密码
	IDs         map[int64]ID                    // 用户 ID -> 权限标记
	HashIndex   map[string]int64                // hash -> uid 反查表, 由 rebuildHashIndexLocked 统一维护, 供 checkHash O(1) 查找
	LatestMIDss map[string]*LatestMIDs          // 频道 -> 最近一次相册边界去重信息, 见 LatestMIDs 注释
	ChannelID   map[string]*ChannelInfo         // 缓存频道名到频道 ID 的映射, 减少重复查询
	HeadCache   map[string]*MediaCache          // 缓存文件头部数据
	TailCache   map[string]*MediaCache          // 缓存文件尾部数据
	MsCache     map[string]*MsCache             // 缓存消息，避免频繁调用 GetMessages
	TCPStatus   struct {
		Bot  TCPStatu
		User TCPStatu
	} // 记录TCP连接状态
	WakeMu struct {
		Bot  sync.Mutex
		User sync.Mutex
	} // 按客户端类别串行化探活与重连, 不占用全局缓存锁
}

// connectStat 按类别返回对应的 TCP 探活状态, 用于收敛调用方重复的 switch cate 判断
func (infos *Infos) connectStat(cate string) *TCPStatu {
	if cate == "user" {
		return &infos.TCPStatus.User
	}
	return &infos.TCPStatus.Bot
}

func (infos *Infos) connectMu(cate string) *sync.Mutex {
	if cate == "user" {
		return &infos.WakeMu.User
	}
	return &infos.WakeMu.Bot
}

var infos *Infos
var offSets *OffSets
var startTime time.Time
var searchCount atomic.Int64
var version = "v1.1.6"

// main 是程序的入口函数
func main() {
	startTime = time.Now()
	// 解析命令行参数
	files := flag.String("files", "files", "配置文件所属目录路径（包含 config.json, session 等）")
	file := flag.String("log", "", "日志文件的存放路径")
	var ver bool
	flag.BoolVar(&ver, "version", false, "显示程序版本号并退出")
	flag.BoolVar(&ver, "v", false, "显示程序版本号并退出")
	flag.Parse()

	// 版本检查逻辑
	if ver {
		fmt.Println(version)
		return
	}

	// 1. 初始化全局 Infos 对象并加载配置
	value, err := newInfos(*file, *files)
	if err != nil {
		log.Printf("初始化失败: %+v", err)
		return
	}
	infos = value
	offSets = newOffSets()

	// 2. 退出时的资源清理（延迟执行）
	defer func() {
		if infos.File != nil {
			if err := infos.File.Close(); err != nil {
				log.Printf("关闭日志文件错误: %+v", err)
			}
		}
		if client := infos.BotClient.Load(); client != nil {
			if err := client.Disconnect(); err != nil {
				log.Printf("Bot 退出失败: %+v", err)
			}
		}
		if client := infos.UserClient.Load(); client != nil {
			if err := client.Disconnect(); err != nil {
				log.Printf("UserBot 退出失败: %+v", err)
			}
		}
	}()

	// 3. 校验关键配置参数
	conf := infos.Conf.Load()
	if conf.AppID == 0 || conf.AppHash == "" || conf.BotToken == "" || conf.UserID == 0 {
		log.Panicf("配置文件缺少必要的参数: AppID、AppHash、BotToken、UserID")
	}

	if conf.Site != "" && !strings.HasPrefix(conf.Site, "http://") && !strings.HasPrefix(conf.Site, "https://") {
		log.Panicf("配置文件 site 必须以 http:// 或 https:// 开头")
	}

	if conf.Port == 0 {
		// 仅填充内存中的默认值, 不落盘持久化（用户未显式设置时不应改写 config.json）
		filled := *conf
		filled.Port = 8080 // 默认端口 8080
		conf = &filled
		infos.Conf.Store(conf)
	}

	// 4. 启动 Bot 客户端
	if err = infos.startBot(); err != nil {
		return
	}

	// 5. 初始化 UserBot 客户端（此时只是连接, 尚未完成登录流程）
	if err = infos.userBotClient(); err != nil {
		log.Printf("UserBot 启动失败: %+v", err)
		return
	}

	// 忽略 SIGPIPE 信号, 防止由于网络异常断开导致进程崩溃
	signal.Ignore(syscall.SIGPIPE)

	// 设置系统中断信号监听, 用于优雅退出
	statusChan := make(chan os.Signal, 1)
	signal.Notify(statusChan, os.Interrupt, syscall.SIGTERM)

	// 创建 HTTP 服务器
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", conf.Port),
		Handler:           http.HandlerFunc(handleMain),
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       600 * time.Second,
		MaxHeaderBytes:    1 << 20, // 最大头部字节数 (1MB)
	}

	// 6. 在独立协程中启动 HTTP 服务
	go func() {
		log.Printf("HTTP 服务运行在 %d 端口", conf.Port)
		if err := server.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Printf("HTTP 服务启动失败: %+v", err)
			} else {
				log.Printf("HTTP 服务已关闭")
			}
			statusChan <- os.Interrupt
		}
	}()

	// 7. 发送程序启动通知
	sendMS(nil, "程序已启动", nil, 60)

	// 8. 检查 UserBot 登录状态, 尝试自动登录（若已存在 session）
	if err := infos.checkStatus(); err != nil {
		log.Printf("UserBot 登录失败: %+v", err)
		infos.resetStatus()
	}

	// 阻塞等待直到接收到退出信号
	status := <-statusChan
	log.Printf("收到信号: %v, 正在退出...", status)

	// 设置关闭的超时时间，例如 60 秒
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP 服务关闭异常: %+v", err)
	} else {
		if infos.Conf.Load().DeBUG {
			log.Printf("HTTP 服务已优雅关闭")
		}
	}
	sendMS(nil, "程序已退出", nil, 60)
}

// newInfos 初始化全局 Infos 对象, 加载日志和配置
func newInfos(filePath, filesPath string) (*Infos, error) {
	if filePath != "" {
		filePath = filepath.Clean(filePath)
	}
	filesPath = filepath.Clean(filesPath)

	maxChannel := 16
	maxMedia := 4
	mutex := new(sync.RWMutex)
	infos := &Infos{
		MaxMs:       maxChannel * 16,
		MaxChannel:  maxChannel,
		MaxMedia:    maxMedia,
		FilePath:    filePath,
		FilesPath:   filesPath,
		Mutex:       mutex,
		ConfMu:      new(sync.Mutex),
		LoMu:        new(sync.Mutex),
		Cond:        sync.NewCond(new(sync.Mutex)),
		Code:        make(chan string, 1),
		Pass:        make(chan string, 1),
		HeadCache:   make(map[string]*MediaCache, maxMedia),
		TailCache:   make(map[string]*MediaCache, maxMedia),
		MsCache:     make(map[string]*MsCache, maxChannel*16),
		ChannelID:   make(map[string]*ChannelInfo, maxChannel),
		LatestMIDss: make(map[string]*LatestMIDs, maxChannel),
		IDs:         make(map[int64]ID),
		HashIndex:   make(map[string]int64),
		Rex:         regexp.MustCompile(`(?i)(?:FLOOD(?:_PREMIUM)?_WAIT_(\d+)|WAIT(?:\s+OF)?\s*(\d+))`),
	}

	// 创建日志文件
	if filePath != "" {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Printf("无法打开日志文件: %+v", err)
		} else {
			infos.File = file
			// 设置日志输出
			multiWriter := io.MultiWriter(os.Stdout, file)
			log.SetOutput(multiWriter)
		}
	}

	// 加载配置文件
	conf, err := loadConf(filesPath)
	if err != nil {
		log.Fatalf("载入配置文件失败: %+v", err)
	}
	infos.Conf.Store(conf)
	infos.IDs = make(map[int64]ID, len(conf.AdminIDs)+len(conf.WhiteIDs)+1)
	infos.buildIDs()
	infos.buildRexRules()

	// 获取 BotID
	if conf.BotToken != "" {
		parts := strings.Split(conf.BotToken, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("BotToken 格式错误")
		}
		result := strings.TrimSpace(parts[0])
		infos.BotID, err = strconv.ParseInt(result, 10, 64)
		if err != nil {
			log.Printf("解析 BotID 失败: %+v", err)
		}
	}

	return infos, nil
}

// newOffSets 初始化全局翻页偏移量缓存
func newOffSets() *OffSets {
	return &OffSets{
		Mutex:   new(sync.Mutex),
		OffSets: make(map[string]OffSet),
	}
}

// buildRegex 预编译正则规则并缓存到 infos.RexRules
func (infos *Infos) buildRexRules() {
	conf := infos.Conf.Load()
	rexRules := make([]*regexp.Regexp, 0, len(conf.Rules))
	for _, rule := range conf.Rules {
		if rule == "" {
			continue
		}
		r, err := regexp.Compile(rule)
		if err != nil {
			log.Printf("正则规则编译失败 [%s]: %+v", rule, err)
			continue
		}
		rexRules = append(rexRules, r)
	}

	infos.Mutex.Lock()
	infos.RexRules = rexRules
	infos.Mutex.Unlock()

	if conf.DeBUG {
		log.Printf("成功预编译 %d 条正则规则", len(rexRules))
	}
}
