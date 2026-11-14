package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xAmarnath/gogram/telegram"
)

// streamPathRe 从 /stream/{mid}/{name} 形式的路径中提取消息 ID, handleParams 每次请求都会用到, 提升为包级变量避免重复编译。
// name 段只允许路径分隔符 / 以外的任意字符（含 . _ % 空格 中文等), 原有 [a-zA-Z0-9]+ 会拒绝含这些字符的文件名
var streamPathRe = regexp.MustCompile(`/stream/(\d+)/[^/]+`)

const timeoutRes = 60 * time.Second

func writeResChunk(w http.ResponseWriter, content []byte, now func() time.Time) (int, error) {
	err := http.NewResponseController(w).SetWriteDeadline(now().Add(timeoutRes))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return w.Write(content)
}

// handleMain 是 HTTP 服务的主分发函数, 根据路径路由到不同的处理器
func handleMain(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// 标准化路径处理, 移除尾部斜杠
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	switch {
	case path == "/":
		// 返回服务器状态 JSON
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		conf := infos.Conf.Load()
		content := map[string]any{
			"版本":   version,
			"域名":   conf.Site,
			"端口":   conf.Port,
			"缓存":   formatFileSize(conf.MaxSize),
			"并发":   conf.Workers,
			"运行时间": handleTime(uint64(time.Since(startTime).Seconds())),
		}
		if err := json.NewEncoder(w).Encode(content); err != nil {
			log.Printf("发送网页失败: %+v", err)
		}
		return
	case path == "/livez", path == "/healthz":
		// k8s liveness 探针: 进程存活即返回 200, 无需鉴权
		// (healthz 已自 Kubernetes v1.16 弃用, 保留作兼容别名)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("发送网页失败: %+v", err)
		}
		return
	case path == "/readyz":
		// k8s readiness 探针: UserBot 已登录(状态 3)才算就绪, 无需鉴权
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if infos.Status.Load() == 3 {
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
				log.Printf("发送网页失败: %+v", err)
			}
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"}); err != nil {
			log.Printf("发送网页失败: %+v", err)
		}
		return
	case path == "/pic":
		handlePic(w, r)
		return
	case path == "/link":
		// 处理链接直链提取并跳转
		handleLink(w, r)
		return
	case path == "/list":
		handleList(w, r)
		return
	case path == "/search":
		// 处理搜索
		handleSearch(w, r)
		return
	case path == "/sources":
		handleSources(w, r)
		return
	case path == "/comments":
		handleComments(w, r)
		return
	case strings.HasPrefix(path, "/stream"):
		// 处理文件分片流式下载（串流播放）核心接口
		handleStream(w, r)
		return
	default:
		// 404
		http.NotFound(w, r)
		return
	}
}

// handleParams 解析流式下载请求参数
func handleParams(r *http.Request) (result Params, err error) {
	params := r.URL.Query()
	if err = checkPass(params); err != nil {
		return result, err
	}

	result.Pass = params.Get("key")
	result.Hash = params.Get("hash")
	result.Cate = params.Get("cate")
	result.Link = params.Get("link")
	result.Keywords = params.Get("keywords")

	values := strings.Split(params.Get("cname"), ",")
	result.Channels = make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result.Channels = append(result.Channels, "@"+strings.TrimLeft(value, "@"))
	}

	values = strings.Split(params.Get("filter"), ",")
	result.Filters = make([]int64, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		size, err := convertSize(value)
		if err != nil {
			size = 0
		}
		result.Filters = append(result.Filters, size)
	}

	page, err := strconv.Atoi(params.Get("page"))
	if err != nil || page < 0 {
		page = 1
	}
	result.Page = page

	// 上限对齐 Telegram 单次消息拉取的合理边界, 防止 limit=10000 之类的请求直接打爆 API 触发 FloodWait
	const maxLimit = 100

	limit, err := strconv.Atoi(params.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 20
	} else if limit > maxLimit {
		limit = maxLimit
	}
	result.Limit = int(limit)

	// ParseInt 出错时返回值已经是 0, 无需再显式判断赋值; 负值视为未提供, 避免 GetMessages 收到非法偏移
	offset, _ := strconv.ParseInt(params.Get("offset"), 10, 32)
	if offset < 0 {
		offset = 0
	}
	result.Offset = int32(offset)

	cid, err := strconv.ParseInt(params.Get("cid"), 10, 64)
	if err != nil {
		cid = 0
	}
	// cid 未提供或显式传 0 时都应回退到 /channel 配置的默认频道, 而不仅仅是解析成功的情形
	if channelID := infos.Conf.Load().ChannelID; cid == 0 && channelID != 0 {
		cid = channelID
	}
	result.CID = cid

	uid, err := strconv.ParseInt(params.Get("uid"), 10, 64)
	if err != nil {
		uid = 0
	}
	result.UID = uid

	mid, err := strconv.ParseInt(params.Get("mid"), 10, 32)
	if err != nil || mid <= 0 {
		matches := streamPathRe.FindStringSubmatch(r.URL.Path)
		if len(matches) == 2 {
			mid, err = strconv.ParseInt(matches[1], 10, 32)
			if err != nil {
				mid = 0
			}
		} else {
			mid = 0
		}
	}
	result.MID = int32(mid)

	// ParseBool 出错时返回值已经是 false, 无需再显式判断赋值
	reverse, _ := strconv.ParseBool(params.Get("reverse"))
	result.Reverse = reverse

	return result, nil
}

// handleRanHeader 将 Range 解析为 handler 可直接使用的完整读取、部分读取或不可满足状态。
// 非 bytes 单范围语法会被忽略并按完整读取处理。
func handleRanHeader(src string, size int64) (start, end int64, status int) {
	if src == "" {
		return 0, size - 1, http.StatusOK
	}
	src = strings.TrimSpace(src)
	if !strings.HasPrefix(src, "bytes=") {
		return 0, size - 1, http.StatusOK
	}
	src = strings.TrimPrefix(src, "bytes=")
	if strings.Contains(src, ",") {
		return 0, size - 1, http.StatusOK
	}

	// 快速路径: bytes=0- 是最常见的整段读取请求, 免去拆分与整数解析
	if src == "0-" && size > 0 {
		return 0, size - 1, http.StatusPartialContent
	}

	parts := strings.SplitN(src, "-", 2)
	if len(parts) < 2 {
		return 0, size - 1, http.StatusOK
	}
	parseNumber := func(value string) (int64, bool) {
		if value == "" {
			return 0, false
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return 0, false
			}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		return n, err == nil
	}

	if parts[0] == "" {
		// bytes=-N: 请求文件末尾 N 字节
		n, valid := parseNumber(parts[1])
		if !valid {
			return 0, size - 1, http.StatusOK
		}
		if n == 0 || size == 0 {
			return size, size - 1, http.StatusRequestedRangeNotSatisfiable
		}
		start = size - n
		if start < 0 {
			start = 0
		}
		end = size - 1
	} else {
		// bytes=A-B 或 bytes=A-
		n, valid := parseNumber(parts[0])
		if !valid {
			return 0, size - 1, http.StatusOK
		}
		if n >= size {
			return n, size - 1, http.StatusRequestedRangeNotSatisfiable
		}
		start = n
		end = size - 1
		if parts[1] != "" {
			n, valid := parseNumber(parts[1])
			if !valid {
				return 0, size - 1, http.StatusOK
			}
			end = n
		}
	}
	if end >= size {
		end = size - 1
	}
	if start > end {
		return start, end, http.StatusRequestedRangeNotSatisfiable
	}
	return start, end, http.StatusPartialContent
}

// writeOOR 按 RFC 7233 返回 416 响应, 必须在 WriteHeader 前设置 Content-Range
func writeOOR(w http.ResponseWriter, size int64) {
	w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
	http.Error(w, "请求范围不合法", http.StatusRequestedRangeNotSatisfiable)
}

func writeRanHeaders(w http.ResponseWriter, start, end, size int64, status int) {
	if status == http.StatusPartialContent {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(status)
}

func handlePic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, fmt.Sprintf("不支持的请求方法: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if params.CID == 0 && len(params.Channels) == 0 {
		http.Error(w, "频道信息无效", http.StatusBadRequest)
		return
	}
	if params.MID == 0 {
		http.Error(w, "消息ID无效", http.StatusBadRequest)
		return
	}

	param := HandleMs{
		CID:    params.CID,
		CNames: params.Channels,
		MIDs:   []int32{params.MID},
		Ctx:    r.Context(),
		Cate:   params.Cate,
		Limit:  1,
	}

	msCache, err := infos.handleMs(param)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	ms := msCache.load()
	cate := msCache.Cate
	client := infos.cateClient(cate)

	if len(ms) == 0 {
		http.Error(w, "未获取到消息", http.StatusBadRequest)
		return
	}
	if client == nil {
		http.Error(w, "对应客户端未就绪", http.StatusServiceUnavailable)
		return
	}

	src := ms[0]
	stat := infos.connectStat(cate)

	// 从媒体中查找最大的 PhotoSize
	var actualThumb telegram.PhotoSize
	var maxSize int32

	switchMax := func(s telegram.PhotoSize) {
		switch sz := s.(type) {
		case *telegram.PhotoSizeObj:
			if sz.Size > maxSize {
				maxSize = sz.Size
				actualThumb = sz
			}
		case *telegram.PhotoSizeProgressive:
			var sMax int32
			for _, m := range sz.Sizes {
				if m > sMax {
					sMax = m
				}
			}
			if sMax > maxSize {
				maxSize = sMax
				actualThumb = sz
			}
		}
	}

	if !src.IsMedia() {
		http.Error(w, "消息不包含媒体", http.StatusBadRequest)
		return
	}
	switch m := src.Media().(type) {
	case *telegram.MessageMediaPhoto:
		if p, ok := m.Photo.(*telegram.PhotoObj); ok {
			for _, s := range p.Sizes {
				switchMax(s)
			}
		}
	case *telegram.MessageMediaDocument:
		if d, ok := m.Document.(*telegram.DocumentObj); ok {
			for _, s := range d.Thumbs {
				switchMax(s)
			}
		}
	}

	if actualThumb == nil {
		http.Error(w, "未找到缩略图", http.StatusNotFound)
		return
	}

	clientIP := handleClientIP(r)
	log.Printf("正在处理来自 %s 的请求, 开始下载封面, cid=%d, mid=%d, name=%s", clientIP, params.CID, params.MID, src.File.Name)

	buf := new(bytes.Buffer)
	// 下载→引用过期刷新→TCP 断开唤醒的公共重试逻辑, 与 handleStream 小文件分支共用
	// 封面使用普通 DC 端点, 避免 media DC 不可达时握手超时后才回退。
	if !infos.downloadMediaR(w, client, cate, &src, param, msCache, buf, &telegram.DownloadOptions{
		NoMediaDC: true,
		ThumbOnly: true,
		ThumbSize: actualThumb,
		Buffer:    buf,
		Ctx:       r.Context(),
	}, "下载封面失败: 文件引用持续过期") {
		return
	}

	stat.touch()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	if n, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
	}
}

// downloadMediaR 下载媒体到 buf, 引用过期自动刷新引用重试、TCP 断开标记失败并异步唤醒主连接。
// 成功返回 true; 失败时已将错误写入 HTTP 响应并返回 false。handlePic 与 handleStream 小文件分支共用。
func (infos *Infos) downloadMediaR(w http.ResponseWriter, client *telegram.Client, cate string, src *telegram.NewMessage, param HandleMs, msCache *MsCache, buf *bytes.Buffer, opts *telegram.DownloadOptions, failSrc string) bool {
	maxCount := 2
	for count := 1; count <= maxCount; count++ {
		version := msCache.Version.Load()
		_, err := client.DownloadMedia(src.Media(), opts)
		if err == nil {
			return true
		}
		switch {
		case telegram.MatchError(err, "FILE_REFERENCE_EXPIRED"):
			if infos.Conf.Load().DeBUG {
				log.Printf("引用过期, 正在尝试刷新文件引用, cid=%d, mid=%d, name=%s", param.CID, param.MIDs[0], src.File.Name)
			}
			newSrc, err := infos.refreshMs(client, version, param, msCache)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return false
			}
			*src = newSrc
			buf.Reset()
		case errors.Is(err, telegram.ErrWorkerTCPDead):
			// 下载连接与主客户端隔离, 池内连接会自行重建, 无需唤醒主连接
			http.Error(w, "下载失败, 连接已断开", http.StatusInternalServerError)
			return false
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			stat := infos.connectStat(cate)
			safeGo(func() {
				stat.fail(client)
				if stat.TCPDead.Load() {
					if err := infos.wakeTCP(client, cate); err != nil {
						log.Printf("TCP 重连失败: %+v", err)
					} else if infos.Conf.Load().DeBUG {
						log.Print("TCP 重连成功")
					}
				}
			})
			return false
		}
	}
	http.Error(w, failSrc, http.StatusInternalServerError)
	return false
}

// handleList 处理来自 HTTP 的文件列表请求
func handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, fmt.Sprintf("不支持的请求方法: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	conf := infos.Conf.Load()
	lenChannels := len(params.Channels)
	lenFilters := len(params.Filters)

	// 与 handleSearch 一致的并发限流: 多频道并行拉取列表会成倍放大 Telegram API 压力,
	// 复用同一份 searchCount 预算, 使 /list 与 /search 的总并发共同受 workers 约束
	maxCount := int64(2 * conf.Workers)
	if maxCount == 0 {
		maxCount = 3
	}

	// 按索引写入各自槽位（不同 goroutine 写不同下标, 无数据竞争）,
	// 全部完成后按下标重组, 保证响应中频道的顺序与请求参数一致
	type channelResult struct {
		item Items
		ok   bool
	}
	results := make([]channelResult, lenChannels)
	var workerPool sync.WaitGroup

	for num, channel := range params.Channels {
		infos.Cond.L.Lock()
		for searchCount.Load() >= maxCount {
			infos.Cond.Wait()
		}
		searchCount.Add(1)
		infos.Cond.L.Unlock()

		workerPool.Add(1)
		safeGo(func() {
			defer func() {
				workerPool.Done()
				searchCount.Add(-1)
				infos.Cond.L.Lock()
				infos.Cond.Broadcast()
				infos.Cond.L.Unlock()
			}()

			// 过滤器与频道按位置一一对应（保持 /list 原有语义, 与 /search 的单值通配不同）
			filter := int64(0)
			if num < lenFilters {
				filter = params.Filters[num]
			}

			item, err := infos.list(channel, params.Page, params.Limit, params.Offset, filter, params.Reverse, r.Context())
			if err != nil {
				log.Printf("获取频道 %s 的文件列表失败: %+v", channel, err)
				return
			}
			results[num] = channelResult{item: item, ok: true}
		})
	}
	workerPool.Wait()

	var items struct {
		HasMore bool    `json:"more"`
		Items   []Items `json:"items"`
	}
	items.Items = make([]Items, 0, lenChannels)
	for _, result := range results {
		if !result.ok {
			continue
		}
		if !items.HasMore {
			items.HasMore = result.item.HasMore
		}
		items.Items = append(items.Items, result.item)
	}
	content, err := json.Marshal(items)
	if err != nil {
		log.Printf("JSON序列化失败: %+v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	n, err := w.Write(content)
	if err != nil {
		log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
		return
	}
}

// handleLink 处理链接提取请求, 将 Telegram 消息链接转换为直链下载地址并执行重定向
func handleLink(w http.ResponseWriter, r *http.Request) {
	res := HackLink{
		Ctx: r.Context(),
	}
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	src := params.Link
	if src == "" || !strings.HasPrefix(src, "http") {
		http.Error(w, "无效的链接", http.StatusBadRequest)
		return
	}

	clientIP := handleClientIP(r)
	log.Printf("正在处理来自 %s 的请求, 开始提取直链, link=%s", clientIP, src)

	// 3. 正则匹配并解析链接, 逐条提取, 单条失败不影响其余链接
	res.UID = params.UID
	res.Pass = params.Pass
	res.Hash = params.Hash
	res.Offset = params.Offset

	results := make([]Item, 0)
	for _, match := range teleLinkRe.FindAllStringSubmatch(src, -1) {
		res.Match = match
		items, err := hackLinks(res)
		if err != nil {
			log.Printf("提取直链失败: %+v", err)
			continue
		}
		if len(items) == 0 {
			continue
		}
		results = append(results, items...)
	}

	if len(results) == 0 {
		http.Error(w, "未找到可下载的媒体", http.StatusNotFound)
		return
	}

	sortItems(results, params.Reverse)

	result, err := json.Marshal(results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if n, err := w.Write(result); err != nil {
		log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
	}
}

// streamWaitTimeout 返回消费端等待单个分片时的超时上限。
// 基础值必须覆盖单个分片的最坏下载耗时：冷启动连接握手 + 最多 6 次重试
// （每次 8s 超时 + 3s 退避）≈ 66s；若存在全局 FloodWait, 还需叠加上剩余的
// 等待时间, 否则分片仍在推进（等待/重试）时会被固定短定时器误判为超时截断响应。
func streamWaitTimeout() time.Duration {
	wait := 90 * time.Second
	if remaining := time.Until(time.Unix(infos.WaitUntil.Load(), 0)); remaining > 0 {
		wait += remaining
	}
	return wait
}

func forwardSource(message *telegram.MessageObj) (channelID int64, postID int32, ok bool) {
	if message == nil || message.FwdFrom == nil || message.FwdFrom.ChannelPost == 0 {
		return 0, 0, false
	}
	channel, ok := message.FwdFrom.FromID.(*telegram.PeerChannel)
	if !ok {
		return 0, 0, false
	}
	return channel.ChannelID, message.FwdFrom.ChannelPost, true
}

// handleStream 处理来自 HTTP 的文件流式读取请求
// 该函数实现了 Range 分段下载支持, 允许像播放普通 mp4 文件一样拖动进度条
func handleStream(w http.ResponseWriter, r *http.Request) {
	// 0. 检验 HTTP 请求类型, 过滤非法请求
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, fmt.Sprintf("不支持的请求方法: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}

	// 1-2. 获取 URL 参数、完成身份校验、解析频道 ID 和消息 ID
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if params.CID == 0 && len(params.Channels) == 0 {
		http.Error(w, "频道信息无效", http.StatusBadRequest)
		return
	}
	if params.MID == 0 {
		http.Error(w, "消息ID无效", http.StatusBadRequest)
		return
	}

	param := HandleMs{
		CID:    params.CID,
		CNames: params.Channels,
		MIDs:   []int32{params.MID},
		Ctx:    r.Context(),
		Cate:   params.Cate,
		Limit:  1,
	}

	msCache, err := infos.handleMs(param)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	ms := msCache.load()
	cate := msCache.Cate
	client := infos.cateClient(cate)

	if len(ms) == 0 {
		http.Error(w, "未获取到消息", http.StatusBadRequest)
		return
	}
	if client == nil {
		http.Error(w, "对应客户端未就绪", http.StatusServiceUnavailable)
		return
	}

	src := ms[0]
	if src.File == nil {
		http.Error(w, "消息不是有效的媒体文件", http.StatusBadRequest)
		return
	}

	// 转发消息统一解析到源消息：让 CID/MID、文件大小/名称与媒体引用全部指向源频道,
	// 避免大文件分支（重定向源 post）与小文件分支（直接下载转发）行为分叉,
	// 也避免媒体缓存键（源 post）与 msCache 键（转发消息）互相错位
	streamCID, streamMID := params.CID, params.MID
	sourceCID, sourceMID, hasSource := forwardSource(src.Message)
	if hasSource {
		srcCache, err := infos.handleMs(HandleMs{
			CID:   sourceCID,
			MIDs:  []int32{sourceMID},
			Ctx:   r.Context(),
			Cate:  cate,
			Limit: 1,
		})
		if err != nil {
			if infos.Conf.Load().DeBUG {
				log.Printf("解析转发源消息失败, 回退到转发消息: %+v", err)
			}
		} else if sm := srcCache.load(); len(sm) > 0 && sm[0].File != nil {
			msCache = srcCache
			ms = sm
			src = sm[0]
			cate = srcCache.Cate
			client = infos.cateClient(cate)
			streamCID, streamMID = sourceCID, sourceMID
		}
	}
	size := src.File.Size
	fileName := src.File.Name
	chunkSize := 1 * 1024 * 1024
	// 整个下载分支使用同一份 Workers 快照, 避免判断分支与实际下载并发数在热重载后不一致
	workers := infos.Conf.Load().Workers
	if size < int64(chunkSize*workers) {
		// 与下方大文件分支保持一致的响应头, 否则浏览器/播放器只能靠内容嗅探猜测 Content-Type,
		// 对 mkv/ts 等容器并不可靠, 下载模式也拿不到正确文件名
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", handleMediaCate(fileName))
		disposition := "inline"
		if r.URL.Query().Get("download") == "true" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", contentDis(disposition, fileName))

		// 与大文件分支一致地解析 Range 头, 避免小文件分支明明声明了 Accept-Ranges 却始终整体返回 200,
		// 导致播放器/下载工具的 seek、断点续传在小文件上失效
		ranHeader := r.Header.Get("Range")
		start, end, rangeStatus := handleRanHeader(ranHeader, size)
		if rangeStatus == http.StatusRequestedRangeNotSatisfiable {
			writeOOR(w, size)
			return
		}

		// HEAD 请求只需要头部信息, 文件大小已从消息元数据中获知, 无需真的向 Telegram 发起下载。
		// 带 Range 时须返回 206, 与下方大文件分支保持一致, 否则断点续传/seek 探测会误判
		if r.Method == http.MethodHead {
			writeRanHeaders(w, start, end, size, rangeStatus)
			return
		}

		clientIP := handleClientIP(r)
		log.Printf("正在处理来自 %s 的请求, 开始下载, cid=%d, mid=%d, name=%s", clientIP, params.CID, params.MID, fileName)
		buf := new(bytes.Buffer)
		// 与封面一致, 小文件直接使用普通 DC, 不等待 media DC 握手回退。
		if !infos.downloadMediaR(w, client, cate, &src, param, msCache, buf, &telegram.DownloadOptions{
			NoMediaDC: true,
			Buffer:    buf,
			ChunkSize: int32(chunkSize),
			Threads:   workers,
			Ctx:       r.Context(),
		}, "下载失败: 文件引用持续过期") {
			return
		}
		infos.connectStat(cate).touch()

		content := buf.Bytes()
		if rangeStatus == http.StatusOK {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		} else {
			// 下载到的内容以实际大小为准做边界保护, 防止 Telegram 返回的字节数与消息元数据里的 size 有出入导致越界。
			// 实际内容为空时按 RFC 返回 416, 避免负下标/越界切片导致 panic 或响应退化
			if len(content) == 0 {
				writeOOR(w, size)
				return
			}
			rangeStart, rangeEnd := start, end
			if rangeStart < 0 {
				rangeStart = 0
			}
			if rangeEnd >= int64(len(content)) {
				rangeEnd = int64(len(content)) - 1
			}
			if rangeStart > rangeEnd {
				rangeStart = rangeEnd
			}
			content = content[rangeStart : rangeEnd+1]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, size))
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if n, err := writeResChunk(w, content, time.Now); err != nil {
			log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
			return
		}
	} else {
		// 创建新的 Stream 流管理对象
		stream := newStream(r.Context(), client, src.Media(), workers, streamMID, streamCID, src.File.Size, fileName, cate)
		stream.Ms = ms

		// 6. 设置 HTTP 响应头
		w.Header().Set("Accept-Ranges", "bytes") // 启用 Range 支持
		w.Header().Set("Content-Type", handleMediaCate(fileName))

		disposition := "inline"
		if r.URL.Query().Get("download") == "true" {
			disposition = "attachment" // 附件模式下载
		}
		w.Header().Set("Content-Disposition", contentDis(disposition, fileName))

		// 7. 处理 HTTP Range 请求（分段读取的核心逻辑）
		ranHeader := r.Header.Get("Range")
		start, end, rangeStatus := handleRanHeader(ranHeader, size)
		// 必须在写出任何响应头之前判定, 否则 416 无法替换已发送的 206
		if rangeStatus == http.StatusRequestedRangeNotSatisfiable {
			writeOOR(w, size)
			return
		}

		writeRanHeaders(w, start, end, size, rangeStatus)

		// 提前发送 Header，重置客户端(ExoPlayer)连接超时倒计时
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// 如果是 HEAD 请求, 只返回首部信息后提早结束避免开启流媒体下载协程
		if r.Method == http.MethodHead {
			return
		}

		clientIP := handleClientIP(r)
		log.Printf("正在处理来自 %s 的请求, 开始下载, cid=%d, mid=%d, name=%s, start=%d, end=%d", clientIP, params.CID, params.MID, fileName, start, end)

		// 缓存逻辑：检查头部/尾部缓存是否命中, 并决定实际下载起点
		stream.HeadSize, stream.TailSize = mediaCacheSizes(size, stream.MaxCacheSize)

		// 启动并发下载协程
		go stream.start(start, end)
		defer func() {
			if stream.Version.Load() > 0 {
				// stream.Ms 由 stream.refresh() 在 stream.Mutex 保护下并发写入
				// （下载协程可能在客户端已断开、本函数已经在准备 return 时仍在后台刷新引用），
				// 这里必须持同一把锁读取，否则可能读到撕裂的 slice header
				stream.Mutex.Lock()
				ms := stream.Ms
				stream.Mutex.Unlock()

				infos.Mutex.Lock()
				msCache.setMes(ms)
				msCache.Time = time.Now()
				msCache.Version.Add(1)
				infos.Mutex.Unlock()
				if infos.Conf.Load().DeBUG {
					log.Printf("缓存数据更新, cid=%d, mid=%d, name=%s, version=%d", params.CID, params.MID, fileName, msCache.Version.Load())
				}
			}

			// 异步清理：不阻塞当前请求 goroutine 返回，使新请求能立即被处理
			safeGo(stream.clean)
			// TCP 断开 → 立即唤醒（无论当前请求成功与否）
			if infos.connectStat(cate).TCPDead.Load() {
				safeGo(func() {
					if err := infos.wakeTCP(client, cate); err != nil {
						log.Printf("TCP 重连失败: %+v", err)
					} else if infos.Conf.Load().DeBUG {
						log.Print("TCP 重连成功")
					}
				})
			}
		}()

		// 10. 循环从下载管道读取分片并写入 HTTP 响应体
		if r.Method == http.MethodGet {
			// 首个分片给更长超时，容忍冷启动 Telegram 连接重建延迟
			timer := time.NewTimer(streamWaitTimeout())
			defer timer.Stop()
			for {
				select {
				case <-r.Context().Done():
					// 客户端断开连接（如浏览器关闭或拖动进度条导致旧请求作废）
					if infos.Conf.Load().DeBUG {
						log.Printf("流式传输文件已取消: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
					}
					return
				case task := <-stream.Tasks:
					// 读取一个下载好的分片任务; stream.Tasks 从不 close()/发送 nil, 无需判空。
					// 注意此处不读 task.Error: worker 总是先推入通道再下载,
					// 此刻 Error 必为 nil, 而其后续写入与本处读取无同步关系;
					// 真正的错误判定统一放在下方通道关闭(!ok)分支, 该处有 close 建立的 happens-before 保证
					// 等待任务完成或者客户端断开。
					// 弹出任务后立刻用自适应超时重新计时：等待区间须覆盖该分片完整的
					// 下载耗时（含重试与 FloodWait）, 否则分片仍在推进时会被误判为超时截断响应
					timer.Reset(streamWaitTimeout())
					select {
					case <-r.Context().Done():
						if infos.Conf.Load().DeBUG {
							log.Printf("流式传输文件已取消: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
						}
						return
					case <-timer.C:
						log.Printf("流式传输文件超时: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
						return
					case content, ok := <-task.Content:
						if !ok {
							// 通道关闭有两种可能: 正常完成, 或 worker 下载失败后先设置 task.Error
							// 再 close(task.Content)。Go 内存模型保证 close 先于"读到零值",
							// 因此此刻读取 task.Error 是安全的。失败时必须中止响应并如实记录,
							// 否则会按已声明的 Content-Length 静默截断文件, 日志还误报"已完成"
							if err := task.Error; err != nil {
								log.Printf("切片下载出错: cid=%d, mid=%d, start=%d, end=%d, name=%s, error=%+v", params.CID, params.MID, task.ContentStart, task.ContentEnd, fileName, err)
								return
							}
							if infos.Conf.Load().DeBUG {
								log.Printf("流式传输文件已完成: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
							}
							return
						}

						// 写入响应
						if len(content) > 0 {
							if _, err := writeResChunk(w, content, time.Now); err != nil {
								log.Printf("写入文件流时出错: cid=%d, mid=%d, name=%s, err=%v", params.CID, params.MID, fileName, err)
								return
							}
						}
						// 检查是否已经写完当前请求的所有范围
						if task.ContentEnd >= end {
							log.Printf("流式传输文件已完成: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
							status := infos.connectStat(cate)
							if status.TCPDead.Load() {
								if err := infos.wakeTCP(client, cate); err != nil {
									log.Printf("TCP 重连失败: %+v", err)
								} else if infos.Conf.Load().DeBUG {
									log.Print("TCP 重连成功")
								}
							} else {
								status.touch()
							}
							return
						}
						task = nil
						content = nil
						timer.Reset(streamWaitTimeout())
					}
				case <-timer.C:
					log.Printf("流式传输文件超时: cid=%d, mid=%d, name=%s", params.CID, params.MID, fileName)
					return
				}
			}
		}
	}
}

// handleSources 获取相册中的所有文件
func handleSources(w http.ResponseWriter, r *http.Request) {
	// 0. 检验 HTTP 请求类型, 过滤非法请求
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, fmt.Sprintf("不支持的请求方法: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}

	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if params.CID == 0 && len(params.Channels) == 0 {
		http.Error(w, "频道信息无效", http.StatusBadRequest)
		return
	}

	if params.MID == 0 {
		http.Error(w, "消息ID无效", http.StatusBadRequest)
		return
	}

	param := HandleMs{
		CID:    params.CID,
		CNames: params.Channels,
		MIDs:   []int32{params.MID},
		Ctx:    r.Context(),
		Cate:   params.Cate,
		Limit:  params.Limit,
	}

	msCache, err := infos.handleMs(param)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	resources := msCache.load()
	if len(resources) == 0 {
		http.Error(w, "未获取到消息", http.StatusBadRequest)
		return
	}

	src := resources[0]
	ms, err := src.GetMediaGroup()
	if err != nil {
		log.Printf("提取媒体组错误: %+v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(ms) == 0 {
		http.Error(w, "未获取到媒体组", http.StatusNotFound)
		return
	}

	items := Items{
		HasMore: false,
		Item:    make([]Item, 0, len(ms)),
	}
	if src.Channel != nil {
		items.Channel = strings.TrimSpace(src.Channel.Title)
		items.ID = src.Channel.Username
	} else {
		items.ID = msCache.Username
	}
	filter := int64(0)
	if len(params.Filters) > 0 {
		filter = params.Filters[0]
	}
	for _, m := range ms {
		if isVideoFile(m.File.Ext) && m.File.Size < filter {
			continue
		}
		item := handleItem(m)
		items.Item = append(items.Item, item)
	}
	sortItems(items.Item, params.Reverse)

	content, err := json.Marshal(items)
	if err != nil {
		log.Printf("序列化相册媒体组失败: %+v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	n, err := w.Write(content)
	if err != nil {
		log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
		return
	}
}

// handleSearch 处理搜索请求, 并发搜索多个频道
func handleSearch(w http.ResponseWriter, r *http.Request) {
	if infos.UserClient.Load() == nil {
		http.Error(w, "userBot 未登录, 无法使用搜索功能", http.StatusUnauthorized)
		return
	}
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	src := params.Keywords
	if src == "" {
		http.Error(w, "缺少关键词", http.StatusBadRequest)
		return
	}
	words := strings.Split(src, ",")
	page := params.Page
	offset := params.Offset
	limit := params.Limit

	clientIP := handleClientIP(r)
	log.Printf("正在处理来自 %s 的请求, 开始搜索, page=%d, offset=%d, limit=%d, keywords=%s", clientIP, page, offset, limit, src)

	// 整个搜索请求使用同一份配置快照, Conf 已是原子指针, 无需再靠 Mutex 保护读取
	conf := infos.Conf.Load()
	channels := make([]string, 0, len(conf.Channels))
	if len(params.Channels) == 0 {
		channels = append(channels, conf.Channels...)
	} else {
		channels = append(channels, params.Channels...)
	}

	results := make(chan Items, len(channels))
	var workerPool sync.WaitGroup

	maxCount := int64(2 * conf.Workers)
	if maxCount == 0 {
		maxCount = 3
	}

	lenWords := len(words)
	lenFilters := len(params.Filters)
	for num, channel := range channels {
		infos.Cond.L.Lock()
		for searchCount.Load() >= maxCount {
			infos.Cond.Wait()
		}
		// 必须在同一把锁内完成"检查未超限"与"计数加一", 否则并发的多个请求可能都通过检查后
		// 才各自加一, 导致 searchCount 短暂超过 maxCount, 限流失效
		searchCount.Add(1)
		infos.Cond.L.Unlock()

		workerPool.Add(1)
		channel = strings.TrimLeft(channel, "@")
		channel = fmt.Sprintf("@%s", channel)
		safeGo(func() {
			defer func() {
				workerPool.Done()
				searchCount.Add(-1)
				infos.Cond.L.Lock()
				infos.Cond.Broadcast()
				infos.Cond.L.Unlock()
			}()

			filter := int64(0)
			// 过滤器为单值时对全部频道生效, 多值时按位置与频道一一对应
			if lenFilters == 1 {
				filter = params.Filters[0]
			} else if num < lenFilters {
				filter = params.Filters[num]
			}

			keywords := ""
			// 关键词为单值时作用到所有频道（常见用法: 一个词搜全部频道）;
			// 多值时按位置一一对应; 频道数超出关键词数时, 超出频道无关键词, 走下方跳过逻辑
			if lenWords == 1 {
				keywords = words[0]
			} else if num < lenWords {
				keywords = words[num]
			}

			keywords = strings.TrimSpace(keywords)
			if keywords == "" || keywords == "#" {
				return
			}
			result, err := infos.search(channel, keywords, page, limit, int32(offset), filter, params.Reverse, r.Context())
			if err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case results <- result:
			}
		})
	}

	// 启动一个协程，在所有任务完成后关闭通道
	safeGo(func() {
		workerPool.Wait()
		close(results)
	})

	var items struct {
		HasMore bool    `json:"more"`
		Items   []Items `json:"items"`
	}

	items.Items = make([]Items, 0, len(channels))
	defer func() {
		content, err := json.Marshal(items)
		if err != nil {
			log.Printf("JSON序列化失败: %+v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		n, err := w.Write(content)
		if err != nil {
			log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
			return
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case result, ok := <-results:
			if !ok {
				return
			}
			if len(result.Item) > 0 {
				items.Items = append(items.Items, result)
			}
			if !items.HasMore && result.HasMore {
				items.HasMore = result.HasMore
			}
		}
	}
}

// handleComments 处理评论消息，返回评论消息列表
func handleComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, fmt.Sprintf("不支持的请求方法: %s", r.Method), http.StatusMethodNotAllowed)
		return
	}
	params, err := handleParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if params.MID == 0 {
		http.Error(w, "消息ID无效", http.StatusBadRequest)
		return
	}
	if params.CID == 0 && len(params.Channels) == 0 {
		http.Error(w, "频道信息无效", http.StatusBadRequest)
		return
	}

	param := HandleMs{
		CID:    params.CID,
		CNames: params.Channels,
		MIDs:   []int32{params.MID},
		Ctx:    r.Context(),
		Cate:   "user",
		Limit:  params.Limit,
	}

	msCache, err := infos.handleMs(param)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	ms := msCache.load()
	if len(ms) == 0 {
		http.Error(w, "未获取到消息", http.StatusNotFound)
		return
	}
	ms, hasMore, err := infos.handleComments(params.MID, params.Offset, params.Page, params.Limit, ms)
	if err != nil {
		http.Error(w, "获取评论失败", http.StatusInternalServerError)
		return
	}

	var result struct {
		HasMore bool    `json:"more"`
		Items   []Items `json:"items"`
	}
	result.Items = make([]Items, 0, 1)
	items := Items{
		HasMore: hasMore,
	}
	result.HasMore = items.HasMore
	filter := int64(0)
	if len(params.Filters) > 0 {
		filter = params.Filters[0]
	}
	for _, m := range ms {
		// ms[0] 是 handleMs 直接取来的锚点消息，不像评论区回复那样经过 IsMedia 过滤，
		// 锚点帖子本身可能是纯文本/无媒体消息或频道解析失败，m.File/m.Channel 此时为 nil，
		// 必须先判空再访问 —— handleItem 内部会直接读 m.Channel.ID，同样需要先排除掉
		if m.File == nil || m.Channel == nil {
			continue
		}
		if isVideoFile(m.File.Ext) && m.File.Size < filter {
			continue
		}

		if items.Channel == "" {
			items.Channel = m.Channel.Title
			items.ID = m.Channel.Username
		}
		item := handleItem(m)
		items.Item = append(items.Item, item)
	}
	sortItems(items.Item, params.Reverse)
	result.Items = append(result.Items, items)

	content, err := json.Marshal(result)
	if err != nil {
		log.Printf("JSON序列化失败: %+v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	n, err := w.Write(content)
	if err != nil {
		log.Printf("写入长度 %d 的响应体失败: %+v", n, err)
		return
	}

}

// mediaMimes 视频文件扩展名（小写, 含点）到 MIME 类型的映射。
// 不用 mime.TypeByExtension 是因为其内置表跨平台不一致（Windows 还会读注册表）,
// 且缺少 mkv/ts 等容器类型, 无法保证各平台返回与这里完全一致的映射
var mediaMimes = map[string]string{
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".wmv":  "video/x-ms-wmv",
	".flv":  "video/x-flv",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
	".ts":   "video/mp2t",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".3gpp": "video/3gpp",
	".3gp":  "video/3gpp",
	".mp4":  "video/mp4",
	".m4s":  "video/mp4",
}

// handleMediaCate 根据文件扩展名返回对应的 MIME 类型
func handleMediaCate(fileName string) string {
	if cate, ok := mediaMimes[strings.ToLower(filepath.Ext(fileName))]; ok {
		return cate
	}
	return "application/octet-stream"
}

// hackLinks 是链接解析的核心逻辑，负责将 t.me 链接映射到具体的媒体消息并生成本程序的流地址
func hackLinks(res HackLink) (items []Item, err error) {
	var username string
	var cid int64 // 用于 ResolvePeer 的标识项
	var mid int32 // 消息 ID

	match := res.Match
	// 1. 解析 Chat ID 或 Username
	if match[2] != "" {
		// 如果是 c/(\d+)，代表私有频道链接，需要给 ID 补充前缀 -100
		value, err := strconv.ParseInt("-100"+match[2], 10, 64)
		if err != nil {
			log.Printf("解析频道ID失败: %+v", err)
			if res.M != nil {
				if _, err := res.M.Reply("解析频道ID失败"); err != nil {
					log.Printf("发送消息失败: %+v", err)
				}
			}
			return items, err
		}
		cid = value
	} else {
		// 否则匹配的是公开频道的 username
		channelInfo, err := infos.handleChannel(match[3])
		if err != nil {
			log.Printf("获取频道 %s 信息失败: %+v", match[3], err)
			return items, err
		}
		cid = channelInfo.CID
		username = channelInfo.UserName
	}

	// 2. 解析消息偏移 ID
	value, err := strconv.ParseInt(match[4], 10, 32)
	if err != nil {
		log.Printf("解析消息ID失败: %+v", err)
		return items, err
	}

	mid = int32(value)

	// 3. 使用 UserBot 客户端尝试获取目标消息
	param := HandleMs{
		CID:    cid,
		CNames: []string{username},
		MIDs:   []int32{mid},
		Ctx:    res.Ctx,
		Cate:   "user",
		Limit:  1,
	}

	msCache, err := infos.handleMs(param)
	if err != nil {
		log.Printf("获取消息失败: cid=%v, mid=%d, err=%+v", cid, mid, err)
		return items, err
	}
	ms := msCache.load()

	if len(ms) == 0 {
		log.Printf("未获取到消息: cid=%v, mid=%d", cid, mid)
		err = errors.New("未获取到消息")
		return items, err
	}

	// 4. 处理链接中的评论 (comment) 逻辑
	if match[5] != "" {
		ms, _, err = infos.handleComments(mid, res.Offset, 1, 0, ms)
		if err != nil {
			log.Printf("获取评论失败: cid=%v, mid=%d, err=%+v", cid, mid, err)
			return items, err
		}
	}

	items = make([]Item, 0, len(ms))
	for _, src := range ms {
		if src.Message.GroupedID != 0 {
			medias, err := src.GetMediaGroup()
			if err != nil {
				log.Printf("提取媒体组错误: %+v", err)
			}
			for _, media := range medias {
				items = append(items, handleItem(media))
			}
		} else {
			if !src.IsMedia() {
				log.Printf("消息不包含媒体: cid=%v, mid=%d", cid, mid)
				continue
			}
			items = append(items, handleItem(src))
		}
	}

	if len(items) == 0 {
		err = errors.New("未获取到有效链接")
		log.Printf("未获取到有效链接: cid=%v, mid=%d", cid, mid)
		return items, err
	}
	return items, nil
}
