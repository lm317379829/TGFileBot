package main

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/xAmarnath/gogram/telegram"
)

// buildIDs 根据配置重建 IDs 以支持 O(1) 权限查询
func (infos *Infos) buildIDs() {
	conf := infos.Conf.Load()

	infos.Mutex.Lock()
	defer infos.Mutex.Unlock()

	// 配置里列出的 ID 无论是否已存在于 IDs 中, 都应补齐配置规定的权限标志,
	// 而非仅新增条目：防止已有条目（如会话中途标记过）缺失配置要求的管理员/白名单位
	setAdminWhite := func(id int64) {
		value := infos.IDs[id]
		value.IsAdmin = true
		value.IsWhite = true
		infos.IDs[id] = value
	}
	setWhite := func(id int64) {
		value := infos.IDs[id]
		value.IsWhite = true
		infos.IDs[id] = value
	}

	setAdminWhite(conf.UserID)

	// 检查AdminIDs是否在IDs中
	for _, id := range conf.AdminIDs {
		setAdminWhite(id)
	}

	// 检查WhiteIDs是否在IDs中
	for _, id := range conf.WhiteIDs {
		setWhite(id)
	}

	infos.rebuildHashIndex()
}

// rebuildHashIndex 根据当前 IDs 重建 hash -> uid 反查表, 调用方必须已持有 infos.Mutex 写锁。
// 碰撞时保留较小 UID（遍历顺序确定, 保证跨重启结果一致）; 被碰撞者的链接鉴权
// 走 calculateHash 直接计算, 不受反查表缺失影响
func (infos *Infos) rebuildHashIndex() {
	index := make(map[string]int64, len(infos.IDs))
	uids := make([]int64, 0, len(infos.IDs))
	for uid := range infos.IDs {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		if hash := infos.calculateHash(uid); hash != "" {
			if owner, ok := index[hash]; ok && owner != uid {
				continue
			}
			index[hash] = uid
		}
	}
	infos.HashIndex = index
}

func (infos *Infos) isAdmin(id int64) bool {
	infos.Mutex.RLock()
	defer infos.Mutex.RUnlock()
	if value, ok := infos.IDs[id]; ok {
		return value.IsAdmin
	}
	return false
}

// needAdmin 校验发送者是否为管理员, 非管理员自动回复权限提示后返回 false
func (infos *Infos) needAdmin(m *telegram.NewMessage) bool {
	if infos.isAdmin(m.SenderID()) {
		return true
	}
	sendMS(m, "你没有使用此命令的权限", nil, 60)
	return false
}

func (infos *Infos) isWhite(id int64) bool {
	infos.Mutex.RLock()
	defer infos.Mutex.RUnlock()
	if value, ok := infos.IDs[id]; ok {
		return value.IsWhite
	}
	return false
}

// calculateHash 为指定用户 ID 生成 6 位 MD5 哈希, 用于鉴权
func (infos *Infos) calculateHash(userID int64) string {
	password := infos.Conf.Load().Password
	if password == "" {
		return ""
	}
	res := fmt.Sprintf("%d%s", userID, password)
	src := md5.Sum([]byte(res))
	return hex.EncodeToString(src[:])[:6]
}

// checkHash 根据哈希值查找对应的用户 ID, 返回 0 表示未找到
// 通过 HashIndex 反查表 O(1) 查找, 而非每次都对 IDs 做线性扫描
func (infos *Infos) checkHash(hash string) int64 {
	if hash == "" {
		return 0
	}
	infos.Mutex.RLock()
	defer infos.Mutex.RUnlock()
	return infos.HashIndex[hash]
}

// checkPass 验证 HTTP 请求中的访问密码或哈希
func checkPass(params url.Values) error {
	confPassword := infos.Conf.Load().Password
	if confPassword != "" {
		hash := params.Get("hash") // 基于用户 ID 的哈希校验
		password := params.Get("key")
		switch {
		case password != "":
			if password != confPassword {
				return errors.New("无效的密码")
			}
		case hash != "":
			uid := infos.checkHash(hash)
			if uid == 0 {
				return errors.New("无效的哈希")
			}
			/*
				value := params.Get("uid")
				uid, err := strconv.ParseInt(value, 10, 64)
				if err == nil && uid != 0 {
					if hash != infos.calculateHash(uid) {
						return errors.New("无效的哈希密码")
					}
				} else {
					log.Printf("UID无效: %s", value)
					return errors.New("无效的UID")
				}
			*/
		default:
			return errors.New("您没有权限访问此链接")
		}
	}
	return nil
}
