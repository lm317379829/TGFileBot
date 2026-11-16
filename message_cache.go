package main

import (
	"fmt"
	"github.com/xAmarnath/gogram/telegram"
	"strconv"
	"strings"
)

// Separate IDs, pagination, query text, and filters so unrelated requests cannot
// accidentally address the same immutable message snapshot.
func messageCacheKey(params HandleMs, username string) string {
	key := params.Cate
	if len(params.CNames) > 0 {
		key += ":" + username
	}
	key += ":" + strconv.FormatInt(params.CID, 10)
	if len(params.MIDs) > 0 {
		ids := make([]string, len(params.MIDs))
		for i, id := range params.MIDs {
			ids[i] = strconv.FormatInt(int64(id), 10)
		}
		key += ":ids=" + strings.Join(ids, ",")
	}
	if params.OffsetID > 0 {
		key += ":offset=" + strconv.FormatInt(int64(params.OffsetID), 10)
	}
	if params.Words != "" {
		key += ":query=" + strconv.Quote(params.Words)
	}
	// Filters affect search/history, while Telegram ignores them for ID lookups.
	if len(params.MIDs) == 0 && params.Filter != nil {
		if _, empty := params.Filter.(*telegram.InputMessagesFilterEmpty); !empty {
			key += fmt.Sprintf(":filter=%T:%#v", params.Filter, params.Filter)
		}
	}
	return key + ":limit=" + strconv.Itoa(params.Limit)
}
