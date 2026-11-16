package main

import (
	"errors"
	"fmt"
	"io"
)

// A short download is an upstream error, not permission to move the requested
// range to another byte. All response headers continue to describe one file.
func bufferedContentRange(content []byte, size, start, end int64) ([]byte, error) {
	if size < 0 || int64(len(content)) != size {
		return nil, fmt.Errorf("下载内容长度与文件信息不符: %w", io.ErrUnexpectedEOF)
	}
	if size == 0 && start == 0 && end == -1 {
		return content, nil
	}
	if start < 0 || end < start || end >= size {
		return nil, errors.New("请求范围超出实际内容")
	}
	return content[start : end+1], nil
}
