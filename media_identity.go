package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/xAmarnath/gogram/telegram"
)

var errMediaChanged = errors.New("文件内容已变化，请重新发起下载")

// contentIdentity describes bytes, not an expiring reference or account-specific
// access hash. Photo size distinguishes variants of the same photograph.
func contentIdentity(media telegram.MessageMedia) (string, error) {
	location, _, size, _, err := telegram.GetFileLocation(media)
	if err != nil {
		return "", err
	}
	switch loc := location.(type) {
	case *telegram.InputDocumentFileLocation:
		if loc != nil && loc.ID != 0 {
			return fmt.Sprintf("document:%d:%d:%s", loc.ID, size, loc.ThumbSize), nil
		}
	case *telegram.InputPhotoFileLocation:
		if loc != nil && loc.ID != 0 {
			return fmt.Sprintf("photo:%d:%d:%s", loc.ID, size, loc.ThumbSize), nil
		}
	}
	return "", errors.New("媒体缺少稳定文件标识")
}

func (stream *Stream) mediaCacheKey() string {
	if stream.ContentIdentity == "" {
		return ""
	}
	return mediaCacheName(stream.CID, stream.MID) + ":" + stream.ContentIdentity
}

func (stream *Stream) setRefreshedMessages(ms []telegram.NewMessage) error {
	if len(ms) == 0 {
		return errors.New("未获取到消息")
	}
	if !ms[0].IsMedia() {
		return errors.New("消息不包含媒体")
	}
	refreshedIdentity, err := contentIdentity(ms[0].Media())
	if err != nil {
		return fmt.Errorf("刷新媒体无效: %w", err)
	}
	if stream.ContentIdentity == "" || refreshedIdentity != stream.ContentIdentity {
		stream.publishMessageSnapshot(ms)
		stream.Error = errMediaChanged
		return errMediaChanged
	}
	stream.Ms = ms
	*stream.Src = ms[0].Media()
	stream.Version.Add(1)
	stream.publishMessageSnapshot(ms)
	return nil
}

func validateMediaRenewal(original, refreshed telegram.MessageMedia) error {
	before, err := contentIdentity(original)
	if err != nil {
		return fmt.Errorf("原媒体无效: %w", err)
	}
	after, err := contentIdentity(refreshed)
	if err != nil {
		return fmt.Errorf("刷新媒体无效: %w", err)
	}
	if before != after {
		return errMediaChanged
	}
	return nil
}

func (stream *Stream) publishMessageSnapshot(ms []telegram.NewMessage) bool {
	if stream.MessageCache == nil {
		return false
	}
	infos.Mutex.Lock()
	defer infos.Mutex.Unlock()
	if stream.MessageCache.Version.Load() != stream.MessageCacheVersion {
		return false
	}
	stream.MessageCache.setMes(ms)
	stream.MessageCache.Time = time.Now()
	stream.MessageCacheVersion = stream.MessageCache.Version.Add(1)
	return true
}
