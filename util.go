package main

import (
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	videoExt = map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".wmv": true, ".flv": true,
		".f4v": true, ".webm": true, ".m4v": true, ".mov": true, ".3gp": true,
		".ts": true, ".m3u8": true, ".rm": true, ".rmvb": true, ".iso": true,
	}

	// teleLinkRe 匹配 t.me 消息链接, 供 handleLink 与 handleMess 复用, 避免每次请求重复编译
	teleLinkRe = regexp.MustCompile(`t\.me\/(c\/(\d+)|([a-zA-Z0-9_]+))\/(\d+)(?:.*(?:comment|thread)=(\d+))?`)
)

// isVideoFile 判断文件后缀是否为视频文件
func isVideoFile(ext string) bool {
	return videoExt[strings.ToLower(ext)]
}

// safeGo 以受保护的方式启动一个后台协程:
// 在该协程内发生 panic 时恢复并记录堆栈, 避免整个进程崩溃。
// note: 仅保护本协程; 协程内部用 defer 注册的资源清理(锁/WaitGroup.Done/Cond.Broadcast 等)
// 会在外层 recover 之前执行(Go defer 后注册先执行), 因此 panic 时资源仍能正常归还。
func safeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("协程崩溃(Panic)已恢复: %v\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}

// handleTime 将秒数格式化为人类可读的时间字符串
func handleTime(secs uint64) string {
	if secs > 86400 {
		return fmt.Sprintf("%dd %dh %dm %ds", secs/86400, (secs%86400)/3600, (secs%3600)/60, secs%60)
	} else if secs > 3600 {
		return fmt.Sprintf("%dh %dm %ds", secs/3600, (secs%3600)/60, secs%60)
	} else if secs > 60 {
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	}
	return fmt.Sprintf("%ds", secs)
}

// formatFileSize 将字节数格式化为 B/K/M/G 单位的字符串, 与 convertSize 支持的单位保持一致
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}

	units := []string{"B", "K", "M", "G"}
	var count int
	var result = float64(size)

	for result >= unit && count < len(units)-1 {
		result /= unit
		count++
	}

	// 如果是整数则不保留小数, 有小数则保留两位
	if result == float64(int64(result)) {
		return fmt.Sprintf("%.0f%s", result, units[count])
	}
	return fmt.Sprintf("%.2f%s", result, units[count])
}

// convertMaxSize 将用户输入的缓存大小字符串（如 "32M"）转换为字节数
// 无法识别单位或数值部分时返回 error, 调用方应将其视为格式错误而非静默使用默认值
func convertSize(str string) (int64, error) {
	src := strings.TrimSpace(strings.ToUpper(str))
	src = strings.TrimSpace(strings.TrimSuffix(src, "B"))
	var unit int64 = 1
	if len(src) > 0 {
		switch src[len(src)-1] {
		case 'K':
			unit = 1024
		case 'M':
			unit = 1024 * 1024
		case 'G':
			unit = 1024 * 1024 * 1024
		}
		if unit != 1 {
			src = strings.TrimSpace(src[:len(src)-1])
		}
	}
	value, err := strconv.ParseFloat(src, 64)
	if err != nil {
		return 0, fmt.Errorf("无效的大小 %q: %w", str, err)
	}
	bytes := value * float64(unit)
	if math.IsNaN(bytes) || math.IsInf(bytes, 0) || bytes < 0 || bytes >= math.Exp2(63) {
		return 0, fmt.Errorf("大小超出合法范围: %q", str)
	}
	return int64(bytes), nil
}

// extractContent 从字符串中提取正文与可选的行数参数
// 例如 "error 20" 返回 ("error", &20)；"error" 返回 ("error", nil)；"20" 返回 ("", &20)
func extractContent(src string) (string, *int) {
	src = strings.TrimSpace(src)

	// 1. 如果整个字符串就是一个数字
	if num, err := strconv.Atoi(src); err == nil {
		return "", &num
	}

	// 2. 寻找主体部分最后一个空格
	count := strings.LastIndexByte(src, ' ')
	if count == -1 {
		return src, nil
	}

	// 3. 判断最后一个空格后面那一截是不是数字
	content := src[count+1:]
	if num, err := strconv.Atoi(content); err == nil {
		return src[:count], &num
	}

	return src, nil
}

// readLastLines 从文件尾部开始向前分块读取, 找出匹配 src 正则的最后 count 行。
// 只看最近日志是最常见的用法, 从尾部倒着读可以在命中足够行数后提前结束, 不必像正向扫描那样每次
// 都读完整个日志文件（日志文件很大时差距明显）；只有当匹配行稀疏、需要的行数很多时才会退化为全文件扫描。
func readLastLines(filePath, src string, count int) (lines []string, err error) {
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("关闭文件失败: %+v", err)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	const chunkSize = 64 * 1024
	pos := info.Size()
	var pending []byte   // 跨块的行残余数据（当前已读区间最前面尚未拼出完整行的部分）
	var matched []string // 倒序收集到的匹配行, 越靠近文件末尾越靠前
	first := true

	for pos > 0 && len(matched) < count {
		readSize := int64(chunkSize)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize

		buf := make([]byte, readSize)
		if _, err := file.ReadAt(buf, pos); err != nil {
			return nil, err
		}
		buf = append(buf, pending...)

		segments := strings.Split(string(buf), "\n")
		if first {
			first = false
			// 日志文件通常以换行符结尾（每条 log.Printf 都带 \n）, 切分产生的末尾空段不是真正的一行
			if n := len(segments); n > 0 && segments[n-1] == "" {
				segments = segments[:n-1]
			}
		}

		// segments[0] 可能是跨块的不完整行, 除非已读到文件开头, 否则留到下一轮和更早的数据拼接
		if pos > 0 {
			pending = []byte(segments[0])
			segments = segments[1:]
		} else {
			pending = nil
		}

		for i := len(segments) - 1; i >= 0 && len(matched) < count; i-- {
			line := strings.TrimSuffix(segments[i], "\r") // 兼容 CRLF, 与 bufio.Scanner 默认行为一致
			if re.MatchString(line) {
				matched = append(matched, line)
			}
		}
	}

	// matched 是从文件末尾往前收集的, 反转成正常的文件先后顺序
	lines = make([]string, len(matched))
	for i, line := range matched {
		lines[len(matched)-1-i] = line
	}
	return lines, nil
}

// cleanFiles 清理指定类型的 session 或 cache 文件
func cleanFiles(realm CleanRealm) {
	switch strings.ToLower(realm.Realm) {
	case "cache":
		if files, err := os.ReadDir(infos.FilesPath); err == nil {
			src := fmt.Sprintf("%s_", strings.ToLower(realm.Cate))
			for _, file := range files {
				name := strings.TrimSpace(file.Name())
				if !file.IsDir() && strings.HasPrefix(name, src) && strings.HasSuffix(name, ".cache") {
					if realm.Filter {
						if realm.ID != "" && realm.ID != "0" {
							currentID := strings.TrimSuffix(strings.TrimPrefix(name, src), ".cache")
							if currentID != realm.ID {
								if err := os.Remove(filepath.Join(infos.FilesPath, name)); err != nil {
									log.Printf("删除缓存文件失败: %+v", err)
								}
							}
						}
					} else {
						if err := os.Remove(filepath.Join(infos.FilesPath, name)); err != nil {
							log.Printf("删除缓存文件失败: %+v", err)
						}
					}
				}
			}
		}
	case "session":
		name := fmt.Sprintf("%s.session", strings.ToLower(realm.Cate))
		if err := os.Remove(filepath.Join(infos.FilesPath, name)); err != nil {
			log.Printf("删除缓存文件失败: %+v", err)
		}
	}
}

// isNumber 判断 rune 是否为数字字符（供 submitCode 过滤验证码使用）
func isNumber(r rune) bool {
	return r >= '0' && r <= '9'
}

// isAllNumber 判断字符串是否全为数字
func isAllNumber(s string) bool {
	for _, r := range s {
		if !isNumber(r) && r != '-' && r != '+' {
			return false
		}
	}
	return true
}

// handleClientIP 从http.Request中提取客户端真实IP，支持代理场景和IPv6
func handleClientIP(r *http.Request) string {
	// 1. 优先处理X-Forwarded-For（代理场景）
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		// 格式："clientIP, proxy1IP, proxy2IP"，取第一个非空IP
		parts := strings.Split(xForwardedFor, ",")
		for _, part := range parts {
			ip := strings.TrimSpace(part)
			if ip != "" {
				return ip
			}
		}
	}

	// 2. 其次处理X-Real-IP（代理常用）
	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP != "" {
		return xRealIP
	}

	// 3. 最后从RemoteAddr获取（直接连接场景）
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}

	// 4. 所有方式失败时返回默认值
	return "未知IP"
}

// evictOldest 为下一次插入腾出容量。返回 false 表示容量禁用，调用方不得插入。
func evictOldest[T any](cache map[string]*T, maxCount int, timeOf func(*T) time.Time, label string) bool {
	if maxCount <= 0 {
		clear(cache)
		return false
	}
	if len(cache) < maxCount {
		return true
	}
	var oldestKey string
	var oldestTime time.Time
	for k, v := range cache {
		t := timeOf(v)
		if oldestKey == "" || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
		}
	}
	if oldestKey != "" {
		delete(cache, oldestKey)
		log.Printf("%s已淘汰最旧条目: key=%s", label, oldestKey)
	}
	return true
}

func evictOldestMediaCache(cache map[string]*MediaCache, maxCount int) bool {
	return evictOldest(cache, maxCount, func(v *MediaCache) time.Time { return v.Time }, "媒体缓存")
}

func evictOldestMsCache(cache map[string]*MsCache, maxCount int) bool {
	return evictOldest(cache, maxCount, func(v *MsCache) time.Time { return v.Time }, "消息缓存")
}

func evictOldestChannelCache(cache map[string]*ChannelInfo, maxCount int) bool {
	return evictOldest(cache, maxCount, func(v *ChannelInfo) time.Time { return v.Time }, "频道缓存")
}

func evictOldestLatestGID(cache map[string]*LatestMIDs, maxCount int) bool {
	return evictOldest(cache, maxCount, func(v *LatestMIDs) time.Time { return v.Time }, "相册去重缓存")
}

// mediaCacheName 生成缓存 key
func mediaCacheName(cid int64, mid int32) string {
	return fmt.Sprintf("%d:%d", cid, mid)
}

// mediaCacheSizes 根据文件大小及配置的最大缓存(maxCacheSize)计算头部缓存和尾部缓存的大小。
// 每侧缓存受 min(maxCacheSize/2, 8MB) 的上限约束, 使 /size 命令配置的缓存大小真正生效。
func mediaCacheSizes(size, maxCacheSize int64) (headSize int64, tailSize int64) {
	if maxCacheSize <= 0 {
		maxCacheSize = 32 * 1024 * 1024
	}
	sideCap := maxCacheSize / 2
	if sideCap > 8*1024*1024 {
		sideCap = 8 * 1024 * 1024
	}

	switch {
	case size < 2*1024*1024:
		return
	case size < 16*1024*1024:
		half := size / 1024 / 2 * 1024
		if half > sideCap {
			half = sideCap
		}
		headSize, tailSize = half, half
	default:
		headSize, tailSize = sideCap, sideCap
	}
	return
}

// handleOffset 处理消息偏移量
func handleOffset(act, kname string, value int32) (offset int32) {
	offSets.Mutex.Lock()
	defer offSets.Mutex.Unlock()
	switch strings.ToLower(act) {
	case "clearprefix":
		for key := range offSets.OffSets {
			if strings.HasPrefix(key, kname) {
				delete(offSets.OffSets, key)
			}
		}
	case "get":
		if values, ok := offSets.OffSets[kname]; ok {
			if time.Since(values.Time) < time.Hour {
				offset = values.Offset
			} else {
				delete(offSets.OffSets, kname)
			}
		}
	case "set":
		if len(offSets.OffSets) >= 32 {
			var oldestKname string
			var oldestTime time.Time
			for k, v := range offSets.OffSets {
				if oldestTime.IsZero() || v.Time.Before(oldestTime) {
					oldestTime = v.Time
					oldestKname = k
				}
			}
			if !oldestTime.IsZero() {
				delete(offSets.OffSets, oldestKname)
			}
		}
		offSets.OffSets[kname] = OffSet{
			Offset: value,
			Time:   time.Now(),
		}
	}
	return
}

func sortItems(items []Item, reverse bool) {
	sort.Slice(items, func(a, b int) bool {
		if reverse {
			// true 时从小到大
			return items[a].MID < items[b].MID
		}
		// false 时从大到小
		return items[a].MID > items[b].MID
	})
}

// contentDis 构造安全的 Content-Disposition 头。
// 对 ASCII 回退字段中的引号/反斜杠做转义、剔除控制字符（防止破坏 quoted-string 语法或注入首部），
// 并附带 RFC 6266 的 filename* 参数以正确显示中文等非 ASCII 文件名。
func contentDis(disposition, fileName string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, fileName)

	var ascii strings.Builder
	for _, r := range clean {
		switch {
		case r == '\\' || r == '"':
			ascii.WriteByte('\\')
			ascii.WriteRune(r)
		case r > 0x7e:
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}

	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, ascii.String(), url.PathEscape(clean))
}
