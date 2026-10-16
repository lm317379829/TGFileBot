# TGFileBot

[English](README_en.md) | [中文](README.md)

TGFileBot 是一个 TeleBot 和 UserBot 深度结合的开源项目，旨在提供高性能的文件直链提取、媒体分片流式传输以及完善的远程机器人管理功能。

> ⚠️ **重要提示**: 本项目使用了修改版的 [gogram](https://github.com/lm317379829/gogram) 库。

**⭐ 项目特色**：采用生产级并发架构，支持 HTTP Range 分片流式下载、自动引用刷新、多级缓存优化等高级特性。

## 核心功能

- **🚀 高性能流式下载**: 基于协程并发的分片下载技术，支持 HTTP Range 请求，可实现视频在浏览器或播放器中的随拖随播（边看边播）。
- **🔗 智能链接提取**: 支持将 Telegram 消息（图片、文档、视频、音频）直接转换为 HTTP(s) 直链。支持私有频道和公开频道的链接解析。
- **🤖 双模式机器人管理**: 通过 Bot 客户端发送指令，远程管理 UserBot 的生命周期（登录、设置、白名单等），无需操作服务器控制台。
- **🛡️ 完善的权限控制**: 支持多管理员机制及白名单系统，所有敏感功能均受权限保护。
- **🔑 灵活的身份验证**: 支持通过密码（key）或动态哈希（hash）保护直链，防止链接被恶意滥用。
- **♻️ 自动引用刷新**: 针对 Telegram 资源引用过期（`FILE_REFERENCE_EXPIRED`）提供毫秒级自动重连和刷新机制，确保大文件下载不中断。
- **📝 伪静态与播控优化**: 提供 `/stream/{mid}/{filename}` 格式的伪静态链接，优化流媒体文件的识别与加载体验。
- **🔍 频道搜索与列表**: 支持在多个频道中并发全文搜索、逐页浏览、媒体组提取等功能。
- **💾 多级缓存优化**: LRU 消息缓存、头部/尾部分片缓存、频道信息缓存，减少 API 调用。

## 部署

### 1. 获取 API ID 和 API Hash

访问 [my.telegram.org](https://my.telegram.org/) 登录您的 Telegram 账号，创建一个新的应用以获取 `App ID` 和 `App Hash`。

### 2. 获取 Bot Token

在 Telegram 中搜索 `@BotFather`，创建一个新的 Bot 并获取 `Bot Token`。

### 3. 配置 `config.json`

在程序运行目录下或指定目录下创建 `config.json` 文件（可参考 `files/config.json.example`）。

```json
{
  "port": 8080,
  "id": 123456789,
  "hash": "your_api_hash_here",
  "site": "https://example.com",
  "proxy": "",
  "botToken": "123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefg",
  "userID": 987654321,
  "password": "your_optional_password",
  "dc": 0,
  "workers": 2,
  "maxSize": 33554432,
  "channelID": 0,
  "adminIDs": [987654321],
  "whiteIDs": [987654321],
  "channels": [],
  "rules": [],
  "debug": false
}
```

**完整参数说明**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `port` | 整数 | 否 | 8080 | HTTP 服务监听端口 |
| `id` | 整数 | 是 | - | Telegram API ID（从 my.telegram.org 获取）|
| `hash` | 字符串 | 是 | - | Telegram API Hash（从 my.telegram.org 获取）|
| `site` | 字符串 | 是 | - | 反代域名或服务器 IP，用于生成直链，必须包含 http(s) 协议 |
| `botToken` | 字符串 | 是 | - | Bot Token（从 @BotFather 获取）|
| `proxy` | 字符串 | 否 | 空 | 代理服务器地址（支持 SOCKS、HTTP、MTProxy、TG 协议），用于连接 Telegram |
| `userID` | 整数 | 是 | - | 主管理员 Telegram 用户 ID（UserBot 对应的账号）|
| `password` | 字符串 | 否 | 空 | 接口访问密码（若设置则所有 API 调用需鉴权）|
| `dc` | 整数 | 否 | 0 | Telegram 数据中心 ID（1-5，0 表示自动选择，遇连接问题可指定）|
| `workers` | 整数 | 否 | 1 | 并发下载协程数（1-4 推荐，过高易触发风控）|
| `maxSize` | 整数 | 否 | 32MB | 最大缓存大小（字节），同时约束头部/尾部分片缓存的上限分配 |
| `channelID` | 整数 | 否 | 0 | 绑定的默认频道 ID（可在 API 中省略 `cid` 参数）|
| `adminIDs` | 数组 | 否 | [] | 辅助管理员 ID 列表（拥有大部分权限，不含登录权限）|
| `whiteIDs` | 数组 | 否 | [] | 白名单 ID 列表（仅可使用基本功能）|
| `channels` | 数组 | 否 | [] | 搜索频道别名列表（通过 `/add` 命令添加）|
| `rules` | 数组 | 否 | [] | 正则过滤规则列表（用于过滤群组消息）|
| `debug` | 布尔 | 否 | false | 调试模式（启用详细日志输出）|

### 4. 命令行参数

程序支持以下命令行参数：

```bash
go run main.go [options]
```

| 参数 | 说明 |
|------|------|
| `-files <path>` | 指定存放配置文件、会话文件和缓存的目录（默认为 `files`） |
| `-log <path>` | 指定日志文件路径（默认空字符串，表示不记录文件日志；指定后日志同时写入控制台与文件）|
| `-version`, `-v` | 打印程序版本号并退出 |

### 5. 运行项目

#### 本地运行

**直接运行编译好的文件（推荐）**
前往 [Releases](https://github.com/lm317379829/TGFileBot/releases) 页面，下载对应系统的可执行文件，解压后直接运行即可。

**从源码编译运行**
```bash
# 安装依赖
go mod tidy

# 运行程序（默认配置文件位置: ./files/config.json）
go run main.go

# 或指定文件目录和日志路径
go run main.go -files ./files -log ./files/log.log

# 查看版本
go run main.go -v
```

#### Docker 部署

项目已提供预构建的 Docker 镜像：`lm317379829/tgfilebot`。

**方法一：使用 Docker 命令行**

直接拉取镜像并运行：

```bash
docker run -d --name tgfilebot \
  --restart unless-stopped \
  -p 8080:8080 \
  -v $(pwd)/files:/app/files \
  lm317379829/tgfilebot
```

**方法二：使用 Docker Compose（推荐）**

在服务器上新建一个 docker-compose.yml 文件，并写入以下内容：
```yml
version: '3.8'
services:
  tgfilebot:
    image: lm317379829/tgfilebot:latest
    container_name: tgfilebot
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./files:/app/files
```

```bash
# 启动服务
docker-compose up -d

# 查看日志
docker logs -f tgfilebot

# 停止容器
docker stop tgfilebot
```

## 使用方法

### Bot 管理命令

通过 Telegram 向 Bot 发送以下命令进行管理（命令 1 分钟后自动删除）：

| 命令 | 说明 | 权限 | 示例 |
|------|------|------|------|
| `/start` | 查看 UserBot 当前登录状态 | 白名单 | `/start` |
| `/qr` | **推荐** 生成登录二维码，手机扫码即可登录 UserBot | 主管理员 | `/qr` |
| `/phone <手机号>` | 发起手机号登录流程 | 主管理员 | `/phone +8613800138000` |
| `/code <验证码>` | 提交手机验证码（**需混入非数字字符**，详见注意事项）| 主管理员 | `/code 1a2b3c4d5` |
| `/pass <密码>` | 提交账号的二次验证（2FA）密码 | 主管理员 | `/pass mypassword` |
| `/password <key>` | 设置 API 接口访问密码 | 管理员 | `/password newsecret` |
| `/proxy <URL>` | 设置代理（支持 SOCKS、HTTP、MTProxy 和 TG 协议，`off` 关闭）| 管理员 | `/proxy socks5://proxy.example.com:1080` |
| `/dc <ID>` | 指定 UserBot 的数据中心（1-5） | 管理员 | `/dc 2` |
| `/allow <ID>` | 将用户 ID 添加到白名单 | 管理员 | `/allow 123456789` |
| `/disallow <ID>` | 从白名单中移除用户 ID 或按索引（`#N`）删除 | 管理员 | `/disallow #0` 或 `/disallow 123456789` |
| `/channel <ID>` | 动态设置绑定的默认频道 ID | 管理员 | `/channel 1001234567890` |
| `/workers <1-4>` | 动态调整并发下载协程数 | 管理员 | `/workers 2` |
| `/site <URL>` | 动态更新生成直链的域名/反代地址 | 管理员 | `/site https://newdomain.com` |
| `/size <size>` | 动态设置最大缓存大小 | 管理员 | `/size 64M` 或 `/size 100MB` |
| `/info [关键字] [行数]` | 查看系统运行日志（支持关键字过滤，默认 10 行）| 管理员 | `/info error 20` |
| `/check <hash>` | 查看哈希值对应的用户信息 | 管理员 | `/check a1b2c3` |
| `/port <端口>` | 动态设置 HTTP 服务端口（重启后生效）| 管理员 | `/port 8081` |
| `/add <别名>` | 添加搜索频道别名（用于搜索功能）| 管理员 | `/add @mychannel` 或 `/add mychannel` |
| `/del <别名或索引>` | 移除搜索频道别名（`#N` 按索引，否则按别名）| 管理员 | `/del #0` 或 `/del mychannel` |
| `/addrule <正则>` | 添加正则过滤规则（用于过滤群组消息）| 管理员 | `/addrule .*spam.*` |
| `/delrule <索引或内容>` | 移除正则过滤规则（`#N` 按索引，否则按内容）| 管理员 | `/delrule #0` 或 `/delrule .*spam.*` |
| `/list <类别>` | 列出指定类别的配置 | 管理员 | `/list channels` 或 `/list ids` 或 `/list rules` |

### 获取直链

**方式 1: 转发媒体**
- 在 Telegram 中转发媒体消息（图片、视频、文档等）到 Bot
- Bot 会自动识别并回复支持分片流传输的直链

**方式 2: 发送消息链接**
- 发送 Telegram 消息链接（格式: `t.me/c/xxx/yyy` 或 `t.me/username/yyy`）
- Bot 会自动解析链接并生成下载地址

**方式 3: 使用 HTTP API**
- 直接调用 `/link` 接口解析链接并获取直链

## HTTP API 接口

所有接口均由内置 HTTP 服务提供，默认监听 `8080` 端口。若配置了 `password`，则需在请求 URL 中附带鉴权参数。

### `GET /`

返回服务器运行状态，**无需鉴权**。

**响应示例**:
```json
{
  "版本": "v1.1.4",
  "域名": "https://example.com",
  "端口": 8080,
  "缓存": "32 MB",
  "并发": 2,
  "运行时间": "1d 2h 3m 4s"
}
```

---

### `GET /stream` — 流媒体 / 下载接口

核心下载接口，支持 HTTP Range 分段请求，可在浏览器或播放器中实现随拖随播。

**URL 格式**:
```
/stream?cid={cid}&mid={mid}&cate={bot|user}&key={key}&download=true
```

**或使用伪静态格式（更好的播放器兼容性）**:
```
/stream/{mid}/{filename}?cid={cid}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `cid` | 否 | 频道 ID（负数形式，如 `-1001234567890`）。若 `config.json` 中已设置 `channelID` 则可省略 |
| `cname` | 否 | 频道用户名/别名（如 `@mychannel`），与 `cid` 二选一，可解析公开或私有频道 |
| `mid` | 是 | 消息 ID（正整数）|
| `cate` | 否 | 下载客户端选择：`user`（使用 UserBot，可访问私有频道）或 `bot`（默认）。UserBot 未登录时自动回退到 Bot |
| `download` | 否 | 设为 `true` 时以附件模式下载（`Content-Disposition: attachment`），否则为内联播放 |
| `key` | 否* | 明文访问密码（设置了 `password` 时必填其一）|
| `hash` | 否* | 哈希鉴权（设置了 `password` 时必填其一）|

**特点**:
- 支持 HTTP Range 请求（206 Partial Content）
- Range 起点超出文件末尾时按 RFC 7233 返回 `416 Requested Range Not Satisfiable`
- 若消息为转发消息，自动解析源频道并重定向，确保分片下载稳定
- 自动处理文件引用过期，无缝续传

---

### `GET /pic` — 缩略图获取接口

获取指定 Telegram 媒体文件（视频、图片、文档）的最大尺寸缩略图。

**URL 格式**:
```
/pic?cid={cid}&mid={mid}&cate={bot|user}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `cid` | 否 | 频道 ID（负数形式，如 `-1001234567890`）。若 `config.json` 中已设置 `channelID` 则可省略 |
| `cname` | 否 | 频道用户名/别名（如 `@mychannel`），与 `cid` 二选一 |
| `mid` | 是 | 消息 ID（正整数）|
| `cate` | 否 | 下载客户端选择：`user`（使用 UserBot）或 `bot`（默认） |
| `key` / `hash` | 否* | 鉴权参数（设置了 `password` 时必填其一）|

> 该接口返回图片格式通常为 `image/jpeg`。若对应的消息没有缩略图，则返回 `404 Not Found`。

---

### `GET /link` — 链接解析接口

将 Telegram 消息链接解析为直链，支持私有频道和公开频道。

**URL 格式**:
```
/link?link={TG_LINK}&key={key}&hash={hash}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `link` | 是 | 完整的 Telegram 消息链接，格式为 `https://t.me/c/{cid}/{mid}` 或 `https://t.me/{username}/{mid}` |
| `key` | 否* | 明文访问密码(与hash二选一) |
| `hash` | 否* | 哈希鉴权(与key二选一) |
| `offset` | 否 | 评论偏移 ID（用于带评论链接的分页）|
| `reverse` | 否 | 是否反序排序返回结果，默认 `false` |

**支持的链接格式**:
- 私有频道: `https://t.me/c/1234567890/100`
- 公开频道: `https://t.me/channelname/100`
- 包含评论: `https://t.me/channelname/100?comment=50`

**响应示例**:
```json
[
  { 
    "ext": ".mp4",
    "src": "Video Title",
    "name": "example.mp4", 
    "username": "My Channel Name",
    "mid": 100, 
    "cid": -1001234567890, 
    "gid": 0,
    "size": 104857600,
    "date": 1672531200
    },
    { 
    "ext": ".mp4",
    "src": "Video Title2",
    "name": "example2.mp4", 
    "username": "My Channel Name",
    "mid": 101, 
    "cid": -1001234567890, 
    "gid": 0,
    "size": 104857600,
    "date": 1672531200
    }
  ]
```

---

### `GET /list` — 频道内容列表接口

获取指定频道的媒体列表，需 UserBot 已登录。

**URL 格式**:
```
/list?cname={频道别名}&page={页码}&limit={每页数量}&offset={偏移ID}&filter={过滤大小}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `cname` | 是 | 频道别名/用户名（例如 `@channelname` 或 `channelname`），可逗号分隔传多个频道，同时返回多份列表 |
| `page` | 否 | 页码，默认 `1` |
| `offset` | 否 | 结果偏移ID，用于翻页，默认 `0` |
| `limit` | 否 | 每页返回数量，默认 `20`，最大 `100` |
| `filter` | 否 | 过滤文件大小，如 `10M`，仅返回大于此大小的文件，默认 `128K`（仅作用于视频文件；可对多个 `cname` 传入逗号分隔的多个值，与频道一一对应）|
| `reverse` | 否 | 是否反序排列，默认 `false` |
| `key` / `hash` | 否* | 鉴权参数（同上） |

**响应示例**:
```json
{
  "more": false,
  "items": [
    {
      "more": false,
      "id": "mychannel",
      "channel": "My Channel Name",
      "item": [
        { 
          "ext": ".mp4",
          "src": "Video Title",
          "name": "example.mp4", 
          "mid": 100, 
          "cid": -1001234567890, 
          "gid": 0,
          "size": 104857600,
          "date": 1672531200
        }
      ]
    }
  ]
}
```

---

### `GET /search` — 频道内容搜索接口

在已配置的搜索频道中并发全文检索，需 UserBot 已登录。

**URL 格式**:
```
/search?keywords={关键词}&page={页码}&limit={每页数量}&offset={偏移ID}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `keywords` | 是 | 搜索关键词（多个关键词用逗号分隔）|
| `page` | 否 | 页码，默认 `1` |
| `limit` | 否 | 每页返回数量，默认 `20`，最大 `100` |
| `offset` | 否 | 结果偏移ID，用于翻页，默认 `0` |
| `filter` | 否 | 过滤文件大小，默认 `128K`（仅作用于视频文件；可对多个频道传入逗号分隔的多个值，与频道一一对应）|
| `reverse` | 否 | 是否反序排列，默认 `false` |
| `cname` | 否 | 指定搜索的频道别名（逗号分隔多个），不指定则搜索所有已配置频道 |
| `key` / `hash` | 否* | 鉴权参数（同上）|

**响应示例**:
```json
{
  "more": true,
  "items": [
    {
      "id": "mychannel",
      "channel": "My Channel Name",
      "word": "search keyword",
      "item": [
        { 
          "ext": ".mp4",
          "src": "Video Title",
          "name": "example.mp4", 
          "mid": 100, 
          "cid": -1001234567890, 
          "size": 104857600
        }
      ]
    }
  ]
}
```

---

### `GET /sources` — 获取消息媒体组的所有多媒体文件

用于一次性获取媒体组（多图/多视频消息）中的所有文件。

**URL 格式**:
```
/sources?cid={频道ID}&cname={频道用户名}&mid={消息ID}&filter={过滤大小}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `cid` 或 `cname` | 是 | 频道 ID 或用户名（两者二选一）|
| `mid` | 是 | 消息ID |
| `filter` | 否 | 过滤文件大小，默认 `128K`（仅作用于视频文件）|
| `key` / `hash` | 否* | 鉴权参数（同上）|

**响应示例**:
```json
{
  "more": false,
  "id": "mychannel",
  "channel": "My Channel Name",
  "item": [
    {
      "ext": ".mp4",
      "src": "Video Title",
      "name": "example.mp4",
      "mid": 100,
      "cid": -1001234567890,
      "gid": 123456,
      "size": 104857600,
      "date": 1672531200
    }
  ]
}
```

---

### `GET /comments` — 获取消息评论中的所有媒体文件

从消息的评论区域提取媒体文件。

**URL 格式**:
```
/comments?cid={频道ID}&cname={频道用户名}&mid={消息ID}&offset={偏移ID}&page={页码}&filter={过滤大小}&key={key}
```

| 参数 | 必填 | 说明 |
|:---|:---:|:---|
| `cid` 或 `cname` | 是 | 频道 ID 或用户名（两者二选一）|
| `mid` | 是 | 消息ID |
| `offset` | 否 | 评论的偏移 ID（用于分页）|
| `page` | 否 | 页码，默认 `1` |
| `filter` | 否 | 过滤文件大小，默认 `128K`（仅作用于视频文件）|
| `key` / `hash` | 否* | 鉴权参数（同上）|

**响应示例**:
```json
{
  "more": true,
  "items": [
    {
      "id": "commentChannel",
      "channel": "Comment Channel Name",
      "item": [
        {
          "ext": ".mp4",
          "src": "Comment title",
          "name": "example.mp4",
          "mid": 100,
          "cid": -1001234567890,
          "gid": 0,
          "size": 104857600
        }
      ]
    }
  ]
}
```

---

### 💡 身份鉴权说明

若配置了 `password`，访问所有 HTTP 接口时须在 URL 中携带以下任意一种鉴权方式：

| 鉴权方式 | URL 参数 | 说明 | 示例 |
|:---|:---|:---|:---|
| 明文密码 | `&key=yourpassword` | 直接传入配置中的 `password` 值 | `?key=mysecret123` |
| 哈希密码 | `&hash=xxxxxx` | 更安全的方式，避免明文暴露密码 | `?hash=a1b2c3` |

**Hash 计算公式**: `md5(uid + password)` 的前 **6 位**十六进制字符串。这里的 `uid` 是离线计算 hash 时使用的用户 ID，不是 HTTP 请求参数。

**计算示例**:
```
uid = 8888
password = mypass
md5("8888mypass") = "7c......" → 前 6 位为 "7c..."
最终 URL 参数: ?hash=7c....
```

Python 示例代码：
```python
import hashlib

uid = 8888
password = "mypass"
hash_input = str(uid) + password
hash_value = hashlib.md5(hash_input.encode()).hexdigest()[:6]
print(f"?hash={hash_value}")
```

---

## 技术架构亮点

### 1. 生产者-消费者并发模型

本项目采用了 **Producer-Consumer** 模式处理文件流：

- **生产者 (Streamer)**: 多个协程（由 `workers` 参数控制）并发从 Telegram 服务器拉取数据块，每个协程从任务管道中获取下载任务，并将完成的数据块发送到 HTTP 响应层。
- **消费者 (HTTP Handler)**: 按照 HTTP Range 请求的字节序，精准地从任务完成通道读取数据块，并按顺序写入 HTTP 响应体。
- **有序性保证**: 使用 `sync.Cond` 条件变量保护任务管道，确保数据块按字节顺序消费。

### 2. 自动引用刷新机制

针对 Telegram 内部的 `file_reference` 过期问题：

- 当下载过程中遇到 `FILE_REFERENCE_EXPIRED` 错误，自动触发刷新流程
- 重新获取消息以更新文件引用，无需中断传输
- 版本号原子操作确保并发情况下只刷新一次
- 支持数小时级别的长连接传输而不中断

### 3. 多级缓存优化体系

| 缓存类型 | 作用 | 容量 |
|:---|:---|:---|
| **消息缓存** | 减少 Telegram API 调用 | 256 条最近消息 |
| **频道缓存** | 缓存频道 ID 解析结果 | 16 个频道信息 |
| **头部分片缓存** | 优化序列播放和预加载 | 可配置，默认 8-16MB |
| **尾部分片缓存** | 优化快进（拖到末尾）场景 | 可配置，默认 8-16MB |

### 4. 高级网络容错

- **渐进式重试**: 网络超时采用指数退避（500ms → 1s → 1.5s → 2s）
- **Flood 处理**: 智能解析 Telegram 限流错误，自动提取等待时间，全局同步所有协程
- **TCP 断连即时感知**: 通过 `TCPDead` 原子标记实时跟踪连接状态，下载协程检测到 `ErrWorkerTCPDead` 时立即标记断连并触发唤醒
- **智能探活唤醒**: 每次请求前通过 `handleMs` 检查 `TCPDead` 状态，断连时自动 Ping 探活并透明重连；异常退出的请求不擦除断连标记，确保感知不丢失
- **连接超时控制**: TCP 连接建立和空闲连接均设有超时限制，防止连接假死
- **连接池隔离**: 每个下载协程配备独立连接池，避免连接争用；断连时自动重建

### 5. 权限管理架构

```
SuperAdmin (主管理员，拥有所有权限)
├── 登录管理: /qr, /phone, /code, /pass
├── 全局设置: /site, /size, /port, /password
├── 白名单管理: /allow, /disallow
└── 频道/规则管理: /add, /del, /addrule, /delrule

Admin (管理员，除登录外的全部权限)
├── 全局设置
├── 白名单管理
└── 频道/规则管理

White (白名单用户，仅基础访问)
└── /start 查看状态
```

---

## 故障排查

### 问题 1：UserBot 登录失败

**症状**: 发送 `/qr` 后没有反应或提示错误

**排查步骤**:
1. 检查 `config.json` 中 `id` 和 `hash` 是否正确
2. 确保网络连接正常（如需要，配置代理 `/proxy`）
3. 检查指定的 DC 是否可用，尝试 `/dc 2` 或 `/dc 4` 切换
4. 查看日志输出 `tail -f files/log.log`，查看具体错误信息

> 💡 若账号开启了二步验证（2FA），二维码登录不可用（Telegram 限制），程序会提示改用 `/phone <手机号>` 登录。

**解决方案**:
```bash
# 清理缓存和会话文件
rm -f files/user.session files/user.cache
# 重新运行程序
go run main.go
```

### 问题 2：下载速度慢或经常超时

**症状**: `/stream` 接口请求经常 504 或很慢

**原因分析**:
- `workers` 设置过低，并发度不足
- Telegram 限流（FloodWait），程序自动等待
- 网络连接不稳定

**解决方案**:
```bash
# 增加并发数（需谨慎，过高易触发风控）
/workers 3

# 切换数据中心
/dc 1

# 增加缓存大小
/size 64M

# 检查网络连接和代理设置
/proxy socks5://your-proxy:1080
```

### 问题 3：文件引用过期错误 (FILE_REFERENCE_EXPIRED)

**症状**: 下载大文件时中途出现错误

**原因**: Telegram 文件引用自动过期

**解决方案**: 程序已自动处理，会自动刷新引用并重试。如果仍出现问题：
```bash
# 重启程序，清理缓存
docker restart tgfilebot
# 或本地运行时 Ctrl+C 后重新启动
```

### 问题 4：API 接口返回 401 Unauthorized

**症状**: 调用 API 接口时返回 401 错误

**原因**: 密码鉴权失败

**解决步骤**:
1. 确认 `config.json` 中 `password` 不为空
2. 验证 URL 中包含正确的鉴权参数（`key` 或 `hash`）
3. 如果使用 `hash` 鉴权，重新计算 hash 值

```python
# Python 计算 hash 示例
import hashlib
uid = 123456789
password = "mypass"
hash_val = hashlib.md5(f"{uid}{password}".encode()).hexdigest()[:6]
# 使用: ?hash={hash_val}
```

### 问题 5：并发搜索超时

**症状**: 调用 `/search` 接口时返回超时

**原因**: 搜索多个频道时超过客户端请求限制

**解决方案**:
```bash
# 限制搜索频道数量（通过 cname 参数指定单个频道）
/search?keywords=keyword&cname=@mychannel

# 或减少搜索频道
/list channels  # 查看当前频道
/del index      # 删除不常用的频道
```

---

## 性能优化建议

### 1. 并发参数调优

```json
{
  "workers": 2,     // 从 2 开始测试，逐步增加到 3-4
  "maxSize": 32M    // 32-64MB 是较均衡的缓存大小
}
```

### 2. 代理配置

如果在国内或 Telegram 被限制的地区运行：

```bash
/proxy socks5://proxy.example.com:1080
```

### 3. 数据中心选择

```bash
/dc 1   # 中国地区
/dc 2   # 欧洲
/dc 4   # 美国
# 根据网络延迟选择最近的 DC
```

### 4. 缓存预热

首次运行时会建立消息缓存，建议：
- 预先加载常用频道的消息列表
- 等待 1-2 小时让 LRU 缓存稳定

---

## 安全建议

### ⚠️ 重要安全提示

1. **密码管理**
   - 不要在代码中硬编码密码，使用环境变量
   - 定期更改 `password`，重新计算 `hash`
   - 使用 `hash` 鉴权而非明文 `key`

2. **敏感信息保护**
   - 不要分享 `config.json` 文件
   - 不要在公网上暴露此服务（使用反向代理 + HTTPS）
   - API Token、手机号等敏感信息不要记录在日志中

3. **账号安全**
   - 定期检查账号登录位置和活动
   - 启用 Telegram 两步验证 (2FA)
   - 不要在不受信任的代码运行此程序

4. **网络安全**
   ```bash
   # 使用 HTTPS 反向代理（示例 Nginx）
   server {
       listen 443 ssl;
       server_name example.com;
       
       ssl_certificate /path/to/cert.pem;
       ssl_certificate_key /path/to/key.pem;
       
       location / {
           proxy_pass http://localhost:8080;
       }
   }
   ```

---

## 注意事项

### 风控风险

- **频繁下载**: 频繁使用 UserBot 进行大文件下载可能触碰 Telegram 的 API 限制
- **建议配置**: 将 `workers` 设置在合理范围（1-4，推荐 2）
- **间隔下载**: 避免连续不断地大量下载，给账号缓冲时间
- **监控限流**: 观察程序日志中的 `FLOOD_WAIT` 错误，如频繁出现需降低并发

### 验证码输入限制 ⚠️ **重要**

由于 Telegram 的安全策略，当通过 Bot 消息提交 `/code` 验证码时，Telegram 服务器会检测到验证码是通过自动化方式发送的（机器人不是真人），因此会拒绝。

**解决方法（必须手动操作）**：

1. 收到 Telegram 发来的 6 位数字验证码，例如 `12345`
2. 在验证码数字之间**随机插入任意非数字字符**（字母、符号、空格均可）
   - 例如: `12345` → `1a2b3c4d5` 或 `1-2-3-4-5` 或 `1 2 3 4 5`
3. **手动**（非复制粘贴自动化消息）将混淆后的字符串发送给 Bot
   - 命令示例: `/code 1a2b3c4d5`
4. 程序会自动过滤掉所有非数字字符，提取出真正的验证码 `12345` 进行登录

**原理**: Telegram 对「原始验证码字符串」的发送行为进行监控，混入随机字符后，消息内容不再与验证码完全匹配，可绕过该限制。

---

## Bot 与 UserBot 的区别及风险

### Bot (机器人)

- **官方支持**: Telegram 官方提供，通过 @BotFather 创建和管理
- **API 限制**: 功能受 TeleBot API 限制，用于与用户交互、发送消息、管理群组等
- **安全性**: Bot 账户相对安全，无法像普通用户一样登录客户端，也无法访问用户的私人聊天记录
- **使用场景**: 公开服务、自动化任务、信息推送等

### UserBot (用户机器人)

- **模拟用户**: 通过模拟普通 Telegram 用户行为来实现自动化操作
- **权限级别**: 使用用户的 API ID 和 API Hash 登录，拥有与普通用户相同的权限
- **功能强大**: 可执行普通用户能做的所有操作（访问私聊、发送任何类型文件、加入频道等）

### 潜在风险

⚠️ **使用 UserBot 前，请充分了解以下风险**：

| 风险类型 | 描述 | 缓解措施 |
|:---|:---|:---|
| **账号安全** | 将 Telegram 账号凭据（API ID、API Hash、手机号）提供给程序，漏洞或恶意使用可能导致账号被盗 | 定期查看登录记录、启用 2FA、只在受信任的服务器运行 |
| **服务条款** | Telegram 的服务条款可能禁止或限制 UserBot 使用，过度滥用可能导致账号限制甚至永久封禁 | 遵守合理使用政策，避免频繁大量操作 |
| **隐私泄露** | UserBot 可访问账号的所有聊天记录和联系人信息 | 只运行开源代码，自己审计或由信任的人审计 |
| **稳定性** | UserBot 基于逆向工程或非官方 API，容易受 Telegram 更新影响 | 及时更新依赖库 `gogram`，关注社区更新 |

**本项目中 UserBot 主要用于**:
- 获取普通 Bot 无法直接访问的媒体文件直链
- 访问私有频道中的文件
- 通过 UserBot 身份进行高性能流式传输

**建议**:
- 使用专用的 Telegram 账号运行此程序，不要用日常使用的账号
- 定期备份会话文件 (`files/*.session`)，以备不时之需
- 启用 Telegram 账号的两步验证 (Two-Step Verification)
- 仔细检查代码变更，只使用来自可信来源的版本

---

## 许可证

本项目遵循 [Apache 2.0](LICENSE) 许可。

---

## 更新日志

### v1.1.6
- ✅ 修复消息缓存并发写入导致的数据错乱：从“加锁修改后发布不可变快照”改为“**所有写入方在锁内完成写时拷贝与替换**”，确保读取者无锁直读到的是完整新快照或完整旧快照，消除并发修改底层数组的竞争条件。
- ✅ 修复通过 `/phone` 登录时，用户在验证码输入间插入非数字（如 `1a2b3c`）后程序崩溃的问题：增加 `stripNonDigits` 辅助函数，在 `LoginByPhone` 前过滤掉验证码中的所有非数字字符，确保传递给 MTProto 库的仅为纯数字串。
- ✅ 改进 `/link` 命令中多条 `t.me` 链接的解析与格式化：
    - 取消默认的全局去重，允许解析同一消息中出现的**多条独立链接**（例如同一相册的 `t.me/c/...` 和 `t.me/s/...`）。
    - 对每条链接独立调用 `parseTelLink`，保留首次成功的解析结果，避免后续失败覆盖前面已成功的链接。
    - 修正 `formatLink` 逻辑，确保每条链接都独立构建参数并独立发送请求，不再依赖全局哈希（`linkHashes`）来合并结果。
- ✅ 修复群管正则匹配（`parse 群`、`管理`等）时，错误地对**私聊消息**进行了多余处理，现增加 `msg.MessageMediaWebPage` 和 `msg.MessageMediaDocument` 判空，确保仅在群组/频道媒体消息上尝试正则匹配，避免浪费计算并减少误判。
- ✅ 优化日志清理（`/clean`）逻辑，现根据 `CleanRealm` 参数执行“保留特定 ID 以外”或“只删除特定 ID”的清理操作：
    - 保留指定 ID 以外（`Keep != ""`）：遍历所有文件，计算哈希，仅删除 `hash != Keep` 的文件。
    - 只删除指定 ID（`Keep == ""`）：遍历所有文件，计算哈希，仅删除 `hash == Keep` 的文件。
    - 修复 `Keep` 和 `Delete` 参数混淆的逻辑错误，确保过滤逻辑严格对应参数本意。

### v1.1.5
- ✅ 修复多链接提取只保留最后一个结果的问题，每条链接独立解析互不影响
- ✅ 流式下载分片失败时中止响应并如实记录日志，不再按 Content-Length 静默截断文件
- ✅ 修复开启两步验证(2FA)的账号使用二维码登录时的空指针崩溃，现会引导改用 `/phone` 登录
- ✅ 登录流程加锁序列化，并发发送 `/phone`、`/qr` 不再同时发起多个登录
- ✅ 群管正则规则仅在群组/频道中生效，不再误伤私聊消息
- ✅ API `limit` 参数强制上限 100（超出自动截断），并拒绝负数 `offset`/`mid`
- ✅ HTTP Range 请求起点超出文件末尾时按 RFC 7233 返回 `416`
- ✅ 消息缓存改为无锁不可变快照（写时拷贝），高并发下减少内存分配与锁竞争
- ✅ 开启密码保护时，无个人发送者的频道帖子不再生成失效直链（改为明确报错跳过）

### v1.1.4
- ✅ 增强并发下载稳定性
- ✅ 优化缓存管理策略
- ✅ 改进错误处理和日志记录
- ✅ 支持更多搜索过滤选项
- ✅ 优化 TCP 断连检测与自动恢复机制

### v1.0.0
- ✅ 初版发布，支持基本的流式下载和 Bot 管理

---

## 贡献指南

欢迎 PR 和 Issue！请确保：
- 代码符合现有风格
- 添加必要的测试
- 更新相关文档
- 提供清晰的 PR 描述

---

## 常见问题 (FAQ)

**Q: 支持哪些视频格式？**  
A: 支持所有 Telegram 支持的视频格式（MP4、WebM、MKV 等），取决于媒体类型。

**Q: 可以同时多人使用吗？**  
A: 可以，程序支持并发请求和多用户鉴权。

**Q: 支持下载私有频道的文件吗？**  
A: 支持，需要 UserBot 已登录，设置 `cate=user` 参数。

**Q: 程序会保存下载的文件吗？**  
A: 不会，程序仅负责流式转发，不存储任何下载内容。

**Q: 如何监控程序状态？**  
A: 访问 `GET /` 端点、查看日志文件 `files/log.log`，或以管理员及以上身份在机器人对话中使用 `/info` 命令查看。

---

## 获取帮助

- 📖 查看本 README 中的故障排查部分
- 🐛 提交 Issue 报告问题
- 💬 检查已关闭的 Issue 中的类似问题
- 📝 查阅项目源码中的注释和 godoc
