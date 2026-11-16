package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/xAmarnath/gogram/telegram"
)

// buildIDs 根据配置重建 IDs 以支持 O(1) 权限查询
func (infos *Infos) buildIDs() {
	infos.Mutex.Lock()
	defer infos.Mutex.Unlock()
	infos.buildIDsLocked(infos.Conf.Load())
}

// buildIDsLocked derives the complete access state from one configuration.
// Callers hold Mutex, including while publishing a replacement configuration.
func (infos *Infos) buildIDsLocked(conf *Conf) {
	infos.IDs = make(map[int64]ID, len(conf.AdminIDs)+len(conf.WhiteIDs)+1)
	setAdmin := func(id int64) {
		if id != 0 {
			infos.IDs[id] = ID{IsAdmin: true, IsWhite: true}
		}
	}
	setAdmin(conf.UserID)
	for _, id := range conf.AdminIDs {
		setAdmin(id)
	}
	for _, id := range conf.WhiteIDs {
		if id != 0 {
			value := infos.IDs[id]
			value.IsWhite = true
			infos.IDs[id] = value
		}
	}
	infos.rebuildHashIndex()
}

// rebuildHashIndex 根据当前 IDs 重建 hash -> uid 反查表, 调用方必须已持有 infos.Mutex 写锁。
// 碰撞时保留较小 UID（遍历顺序确定, 保证跨重启结果一致）。
func (infos *Infos) rebuildHashIndex() {
	conf := infos.Conf.Load()
	index := make(map[string]int64, 2*len(infos.IDs))
	uids := make([]int64, 0, len(infos.IDs))
	for uid := range infos.IDs {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		hashes := []string{signedHash(uid, conf.Password)}
		if !conf.DisableLegacyHash {
			hashes = append(hashes, legacyHash(uid, conf.Password))
		}
		for _, hash := range hashes {
			if hash == "" {
				continue
			}
			if _, exists := index[hash]; !exists {
				index[hash] = uid
			}
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

// calculateHash signs new links with a versioned 128-bit HMAC token.
func (infos *Infos) calculateHash(userID int64) string {
	return signedHash(userID, infos.Conf.Load().Password)
}

func signedHash(userID int64, password string) string {
	if password == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(password))
	fmt.Fprintf(mac, "tgfilebot:hash:v2:%d", userID)
	return "v2_" + hex.EncodeToString(mac.Sum(nil)[:16])
}

// legacyHash is accepted only while the explicit migration switch permits it.
func legacyHash(userID int64, password string) string {
	if password == "" {
		return ""
	}
	src := md5.Sum([]byte(fmt.Sprintf("%d%s", userID, password)))
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
	infos.Mutex.RLock()
	defer infos.Mutex.RUnlock()
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
			uid := infos.HashIndex[hash]
			if uid == 0 {
				return errors.New("无效的哈希")
			}
		default:
			return errors.New("您没有权限访问此链接")
		}
	}
	return nil
}
