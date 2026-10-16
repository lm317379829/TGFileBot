# TGFileBot

[English](README_en.md) | [中文](README.md)

TGFileBot is an open-source project that deeply integrates a TeleBot and a UserBot, designed to provide high-performance direct link extraction for files, media chunk streaming, and comprehensive remote bot management features.

> ⚠️ **Important Note**: This project uses a modified version of the [gogram](https://github.com/lm317379829/gogram) library.

**⭐ Project Features**: Adopts a production-level concurrent architecture, supports advanced features such as HTTP Range chunk streaming, automatic reference refreshing, and multi-level cache optimization.

## Core Features

- **🚀 High-performance Streaming**: Based on coroutine concurrent chunk download technology, it supports HTTP Range requests, enabling drag-and-play video streaming in browsers or players.
- **🔗 Smart Link Extraction**: Supports converting Telegram messages (images, documents, videos, audios) directly into HTTP(s) direct links. Supports parsing links from private and public channels.
- **🤖 Dual-mode Bot Management**: Remotely manage the UserBot's lifecycle (login, settings, whitelist, etc.) by sending commands via the Bot client, without needing to operate the server console.
- **🛡️ Comprehensive Access Control**: Supports multi-administrator mechanisms and a whitelist system, with all sensitive features protected by permissions.
- **🔑 Flexible Authentication**: Protects direct links via password (key) or dynamic hash to prevent link abuse.
- **♻️ Auto Reference Refreshing**: Provides millisecond-level auto-reconnect and refresh mechanisms for expired Telegram resource references (`FILE_REFERENCE_EXPIRED`), ensuring uninterrupted large file downloads.
- **📝 Pseudo-static and Playback Optimization**: Offers pseudo-static links in the format `/stream/{mid}/{filename}` to optimize media file recognition and loading experience.
- **🔍 Channel Search and Listing**: Supports concurrent full-text search, paginated browsing, and media group extraction across multiple channels.
- **💾 Multi-level Cache Optimization**: LRU message cache, head/tail chunk cache, and channel information cache to reduce API calls.

## Deployment

### 1. Get API ID and API Hash

Visit [my.telegram.org](https://my.telegram.org/) to log in to your Telegram account, create a new application to obtain the `App ID` and `App Hash`.

### 2. Get Bot Token

Search for `@BotFather` in Telegram, create a new Bot, and obtain the `Bot Token`.

### 3. Configure `config.json`

Create a `config.json` file in the program's running directory or the specified directory (refer to `files/config.json.example`).

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

**Full Parameter Description**:

| Parameter | Type | Required | Default | Description |
|------|------|------|--------|------|
| `port` | Integer | No | 8080 | HTTP service listening port |
| `id` | Integer | Yes | - | Telegram API ID (from my.telegram.org) |
| `hash` | String | Yes | - | Telegram API Hash (from my.telegram.org) |
| `site` | String | Yes | - | Reverse proxy domain or server IP for generating direct links; must include http(s) protocol |
| `botToken` | String | Yes | - | Bot Token (from @BotFather) |
| `proxy` | String | No | Empty | Proxy server address (supports SOCKS, HTTP, MTProxy, TG protocols) for connecting to Telegram |
| `userID` | Integer | Yes | - | Main administrator's Telegram User ID (account corresponding to the UserBot) |
| `password` | String | No | Empty | API access password (if set, all API calls require authentication) |
| `dc` | Integer | No | 0 | Telegram Data Center ID (1-5, 0 means auto-select; can be specified if connection issues arise) |
| `workers` | Integer | No | 1 | Number of concurrent download coroutines (1-4 recommended; too high may trigger rate limits) |
| `maxSize` | Integer | No | 32MB | Maximum cache size (bytes); also constrains the upper limit for head/tail chunk cache allocation |
| `channelID` | Integer | No | 0 | Bound default Channel ID (`cid` parameter can be omitted in API) |
| `adminIDs` | Array | No | [] | List of secondary administrator IDs (has most permissions, excluding login permission) |
| `whiteIDs` | Array | No | [] | List of whitelist IDs (can only use basic features) |
| `channels` | Array | No | [] | Search channel alias list (added via `/add` command) |
| `rules` | Array | No | [] | Regex filter rules list (used to filter group messages) |
| `debug` | Boolean | No | false | Debug mode (enables detailed log output) |

### 4. Command Line Arguments

The program supports the following command line arguments:

```bash
go run main.go [options]
```

| Argument | Description |
|------|------|
| `-files <path>` | Specifies the directory for configuration files, session files, and cache (defaults to `files`) |
| `-log <path>` | Specifies the log file path (defaults to an empty string, meaning no file logging; when set, logs are written to both console and file) |
| `-version`, `-v` | Prints the program version number and exits |

### 5. Running the Project

#### Run Locally

**Run the compiled executable directly (Recommended)**
Go to the [Releases](https://github.com/lm317379829/TGFileBot/releases) page, download the executable file for your system, extract it, and run it directly.

**Compile and run from source**
```bash
# Install dependencies
go mod tidy

# Run the program (default config file location: ./files/config.json)
go run main.go

# Or specify the file directory and log path
go run main.go -files ./files -log ./files/log.log

# Check version
go run main.go -v
```

#### Docker Deployment

Pre-built Docker images are provided: `lm317379829/tgfilebot`.

**Method 1: Using Docker CLI**

Pull the image and run directly:

```bash
docker run -d --name tgfilebot \
  --restart unless-stopped \
  -p 8080:8080 \
  -v $(pwd)/files:/app/files \
  lm317379829/tgfilebot
```

**Method 2: Using Docker Compose (Recommended)**

Create a new `docker-compose.yml` file on your server and add the following content:
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
# Start service
docker-compose up -d

# View logs
docker logs -f tgfilebot

# Stop container
docker stop tgfilebot
```

## Usage

### Bot Management Commands

Send the following commands to the Bot via Telegram for management (commands will be automatically deleted after 1 minute):

| Command | Description | Permission | Example |
|------|------|------|------|
| `/start` | View UserBot's current login status | Whitelist | `/start` |
| `/qr` | **Recommended** Generate login QR code; scan with phone to log in UserBot | SuperAdmin | `/qr` |
| `/phone <Phone Number>` | Initiate phone number login process | SuperAdmin | `/phone +8613800138000` |
| `/code <code>` | Submit phone verification code (**must mix in non-numeric characters**, see notes) | SuperAdmin | `/code 1a2b3c4d5` |
| `/pass <password>` | Submit account 2FA password | SuperAdmin | `/pass mypassword` |
| `/password <key>` | Set API interface access password | Admin | `/password newsecret` |
| `/proxy <URL>` | Set proxy (supports SOCKS, HTTP, MTProxy, and TG protocols; `off` to disable) | Admin | `/proxy socks5://proxy.example.com:1080` |
| `/dc <ID>` | Specify UserBot's data center (1-5) | Admin | `/dc 2` |
| `/allow <ID>` | Add user ID to whitelist | Admin | `/allow 123456789` |
| `/disallow <ID>` | Remove user ID from whitelist or delete by index (`#N`) | Admin | `/disallow #0` or `/disallow 123456789` |
| `/channel <ID>` | Dynamically set the bound default channel ID | Admin | `/channel 1001234567890` |
| `/workers <1-4>` | Dynamically adjust concurrent download coroutines | Admin | `/workers 2` |
| `/site <URL>` | Dynamically update the domain/proxy address for direct links | Admin | `/site https://newdomain.com` |
| `/size <size>` | Dynamically set max cache size | Admin | `/size 64M` or `/size 100MB` |
| `/info [keyword] [lines]` | View system running logs (supports keyword filtering, default 10 lines) | Admin | `/info error 20` |
| `/check <hash>` | View user info corresponding to hash | Admin | `/check a1b2c3` |
| `/port <port>` | Dynamically set HTTP service port (takes effect after restart) | Admin | `/port 8081` |
| `/add <alias>` | Add search channel alias (used for search function) | Admin | `/add @mychannel` or `/add mychannel` |
| `/del <alias or index>` | Remove search channel alias (`#N` by index, otherwise by alias) | Admin | `/del #0` or `/del mychannel` |
| `/addrule <regex>` | Add regex filter rule (used to filter group messages) | Admin | `/addrule .*spam.*` |
| `/delrule <index or content>` | Remove regex filter rule (`#N` by index, otherwise by content) | Admin | `/delrule #0` or `/delrule .*spam.*` |
| `/list <category>` | List configs of the specified category | Admin | `/list channels` or `/list ids` or `/list rules` |

### Getting Direct Links

**Method 1: Forward Media**
- Forward media messages (images, videos, documents, etc.) to the Bot in Telegram
- The Bot will automatically recognize them and reply with a direct link that supports chunk streaming

**Method 2: Send Message Link**
- Send a Telegram message link (format: `t.me/c/xxx/yyy` or `t.me/username/yyy`)
- The Bot will automatically parse the link and generate a download address

**Method 3: Use HTTP API**
- Call the `/link` interface directly to parse the link and get the direct link

## HTTP API Interface

All interfaces are provided by the built-in HTTP service, listening on port `8080` by default. If `password` is configured, authentication parameters must be included in the request URL.

### `GET /`

Returns server running status, **no authentication required**.

**Response Example**:
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

### `GET /stream` — Streaming / Download Interface

Core download interface, supports HTTP Range requests, enabling drag-and-play in browsers or players.

**URL Format**:
```
/stream?cid={cid}&mid={mid}&cate={bot|user}&key={key}&download=true
```

**Or use pseudo-static format (better player compatibility)**:
```
/stream/{mid}/{filename}?cid={cid}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `cid` | No | Channel ID (negative format, e.g., `-1001234567890`). Can be omitted if `channelID` is set in `config.json` |
| `cname` | No | Channel username/alias (e.g., `@mychannel`); choose either this or `cid`, can resolve public or private channels |
| `mid` | Yes | Message ID (positive integer) |
| `cate` | No | Download client selection: `user` (uses UserBot, can access private channels) or `bot` (default). Automatically falls back to Bot if UserBot is not logged in |
| `download` | No | Set to `true` to download as attachment (`Content-Disposition: attachment`), otherwise plays inline |
| `key` | No* | Plaintext access password (required if `password` is set, mutually exclusive with hash) |
| `hash` | No* | Hash authentication (required if `password` is set) |

**Features**:
- Supports HTTP Range requests (206 Partial Content)
- Returns `416 Requested Range Not Satisfiable` per RFC 7233 when the Range start lies beyond EOF
- If the message is a forwarded message, it automatically parses the source channel and redirects, ensuring stable chunk downloading
- Automatically handles file reference expiration with seamless resumption

---

### `GET /pic` — Thumbnail Retrieval Interface

Gets the maximum size thumbnail of the specified Telegram media file (video, image, document).

**URL Format**:
```
/pic?cid={cid}&mid={mid}&cate={bot|user}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `cid` | No | Channel ID (negative format, e.g., `-1001234567890`). Can be omitted if `channelID` is set in `config.json` |
| `cname` | No | Channel username/alias (e.g., `@mychannel`); either this or `cid` (choose one) |
| `mid` | Yes | Message ID (positive integer) |
| `cate` | No | Download client selection: `user` (uses UserBot) or `bot` (default) |
| `key` / `hash` | No* | Authentication parameters (required if `password` is set) |

> This interface usually returns an `image/jpeg` format. If the message has no thumbnail, it returns `404 Not Found`.

---

### `GET /link` — Link Parsing Interface

Parses Telegram message links into direct links; supports private and public channels.

**URL Format**:
```
/link?link={TG_LINK}&key={key}&hash={hash}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `link` | Yes | Complete Telegram message link, formatted as `https://t.me/c/{cid}/{mid}` or `https://t.me/{username}/{mid}` |
| `key` | No* | Plaintext access password (mutually exclusive with hash) |
| `hash` | No* | Hash authentication (mutually exclusive with key) |
| `offset` | No | Comment offset ID (for pagination with comment links) |
| `reverse` | No | Whether to sort returned results in reverse, default `false` |

**Supported Link Formats**:
- Private channel: `https://t.me/c/1234567890/100`
- Public channel: `https://t.me/channelname/100`
- Including comment: `https://t.me/channelname/100?comment=50`

**Response Example**:
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

### `GET /list` — Channel Content List Interface

Gets the media list of a specified channel. UserBot must be logged in.

**URL Format**:
```
/list?cname={channel_alias}&page={page}&limit={limit}&offset={offset_id}&filter={size_filter}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `cname` | Yes | Channel alias/username (e.g., `@channelname` or `channelname`); multiple comma-separated channels can be passed to return several lists at once |
| `page` | No | Page number, default `1` |
| `offset` | No | Result offset ID for pagination, default `0` |
| `limit` | No | Return quantity per page, default `20`, max `100` |
| `filter` | No | Filter file size, e.g., `10M`, only returns files larger than this size, default `128K` (only applies to video files; can pass multiple comma-separated values for multiple `cname`, one per channel) |
| `reverse` | No | Whether to sort in reverse, default `false` |
| `key` / `hash` | No* | Authentication parameters (same as above) |

**Response Example**:
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

### `GET /search` — Channel Content Search Interface

Concurrent full-text retrieval in configured search channels. UserBot must be logged in.

**URL Format**:
```
/search?keywords={keywords}&page={page}&limit={limit}&offset={offset_id}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `keywords` | Yes | Search keywords (separate multiple keywords with commas) |
| `page` | No | Page number, default `1` |
| `limit` | No | Return quantity per page, default `20`, max `100` |
| `offset` | No | Result offset ID for pagination, default `0` |
| `filter` | No | Filter file size, default `128K` (only applies to video files; can pass multiple comma-separated values for multiple channels, one per channel) |
| `reverse` | No | Whether to sort in reverse, default `false` |
| `cname` | No | Specify search channel aliases (comma-separated). If not specified, searches all configured channels |
| `key` / `hash` | No* | Authentication parameters (same as above) |

**Response Example**:
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

### `GET /sources` — Get All Multimedia Files of a Message Media Group

Used to obtain all files in a media group (multi-image/multi-video message) at once.

**URL Format**:
```
/sources?cid={channel_ID}&cname={channel_username}&mid={message_ID}&filter={size_filter}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `cid` or `cname` | Yes | Channel ID or username (choose one) |
| `mid` | Yes | Message ID |
| `filter` | No | Filter file size, default `128K` (only applies to video files) |
| `key` / `hash` | No* | Authentication parameters (same as above) |

**Response Example**:
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

### `GET /comments` — Get All Media Files in Message Comments

Extracts media files from a message's comment area.

**URL Format**:
```
/comments?cid={channel_ID}&cname={channel_username}&mid={message_ID}&offset={offset_id}&page={page}&filter={size_filter}&key={key}
```

| Parameter | Required | Description |
|:---|:---:|:---|
| `cid` or `cname` | Yes | Channel ID or username (choose one) |
| `mid` | Yes | Message ID |
| `offset` | No | Comment offset ID (for pagination) |
| `page` | No | Page number, default `1` |
| `filter` | No | Filter file size, default `128K` (only applies to video files) |
| `key` / `hash` | No* | Authentication parameters (same as above) |

**Response Example**:
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

### 💡 Authentication Explanation

If `password` is configured, accessing all HTTP interfaces requires one of the following authentication methods in the URL:

| Auth Method | URL Parameter | Description | Example |
|:---|:---|:---|:---|
| Plaintext password | `&key=yourpassword` | Pass the `password` value configured directly | `?key=mysecret123` |
| Hash password | `&hash=xxxxxx` | More secure method, avoids exposing plaintext password | `?hash=a1b2c3` |

**Hash calculation formula**: The first **6 characters** of the hexadecimal string of `md5(uid + password)`. The `uid` here is the user ID used when calculating the hash offline — it is NOT an HTTP request parameter.

**Calculation Example**:
```
uid = 8888
password = mypass
md5("8888mypass") = "7c......" → First 6 chars are "7c..."
Final URL parameter: ?hash=7c....
```

Python Example Code:
```python
import hashlib

uid = 8888
password = "mypass"
hash_input = str(uid) + password
hash_value = hashlib.md5(hash_input.encode()).hexdigest()[:6]
print(f"?hash={hash_value}")
```

---

## Technical Architecture Highlights

### 1. Producer-Consumer Concurrent Model

This project uses the **Producer-Consumer** pattern to handle file streams:

- **Producer (Streamer)**: Multiple coroutines (controlled by the `workers` parameter) concurrently fetch data chunks from the Telegram server. Each coroutine takes a download task from the task pipeline and sends the completed data chunk to the HTTP response layer.
- **Consumer (HTTP Handler)**: Based on the HTTP Range request's byte order, accurately reads data chunks from the task completion channel and sequentially writes them to the HTTP response body.
- **Ordering Guarantee**: Uses a `sync.Cond` condition variable to protect the task pipeline, ensuring data chunks are consumed in byte order.

### 2. Auto Reference Refreshing Mechanism

Addressing the `file_reference` expiration issue within Telegram:

- When a `FILE_REFERENCE_EXPIRED` error occurs during downloading, it automatically triggers the refresh process.
- Re-fetches the message to update the file reference without interrupting the transfer.
- Atomic operations on version numbers ensure it only refreshes once under concurrent conditions.
- Supports long connections lasting for hours without interruption.

### 3. Multi-level Cache Optimization System

| Cache Type | Function | Capacity |
|:---|:---|:---|
| **Message Cache** | Reduce Telegram API calls | 256 recent messages |
| **Channel Cache** | Cache channel ID parsing results | 16 channel infos |
| **Head Chunk Cache** | Optimize sequential playback and preloading | Configurable, default 8-16MB |
| **Tail Chunk Cache** | Optimize fast-forward (seek to end) scenarios | Configurable, default 8-16MB |

### 4. Advanced Network Fault Tolerance

- **Progressive Retry**: Network timeouts use exponential backoff (500ms → 1s → 1.5s → 2s).
- **Flood Handling**: Intelligently parses Telegram rate limit errors, automatically extracts wait times, and synchronizes all coroutines globally.
- **Instant TCP Disconnection Detection**: Tracks connection status in real-time via a `TCPDead` atomic flag; download coroutines immediately mark disconnection and trigger wake-up upon `ErrWorkerTCPDead`.
- **Intelligent Keep-alive Probing**: `handleMs` checks `TCPDead` before each request, automatically sends a Ping probe on disconnection with transparent reconnection; abnormal exits preserve the disconnection flag to ensure no missed detection.
- **Connection Timeout Control**: Timeout limits for both connection establishment and idle connections to prevent zombie connections.
- **Isolated Connection Pools**: Each download coroutine has an independent connection pool to avoid contention; automatically rebuilt on disconnection.

### 5. Permission Management Architecture

```
SuperAdmin (Main administrator, has all permissions)
├── Login Management: /qr, /phone, /code, /pass
├── Global Settings: /site, /size, /port, /password
├── Whitelist Management: /allow, /disallow
└── Channel/Rule Management: /add, /del, /addrule, /delrule

Admin (Administrator, has all permissions except login)
├── Global Settings
├── Whitelist Management
└── Channel/Rule Management

White (Whitelist user, basic access only)
└── /start View status
```

---

## Troubleshooting

### Issue 1: UserBot Login Failed

**Symptom**: No response or error prompt after sending `/qr`.

**Troubleshooting Steps**:
1. Check if `id` and `hash` in `config.json` are correct.
2. Ensure network connection is normal (if needed, configure proxy `/proxy`).
3. Check if the specified DC is available, try `/dc 2` or `/dc 4` to switch.
4. View log output `tail -f files/log.log` for specific error messages.

> 💡 If the account has Two-Step Verification (2FA) enabled, QR login is unavailable (a Telegram limitation); the program will prompt you to log in via `/phone <number>` instead.

**Solution**:
```bash
# Clear cache and session files
rm -f files/user.session files/user.cache
# Rerun the program
go run main.go
```

### Issue 2: Slow Download Speed or Frequent Timeouts

**Symptom**: `/stream` interface requests often return 504 or are very slow.

**Cause Analysis**:
- `workers` setting is too low, insufficient concurrency.
- Telegram rate limiting (FloodWait), program automatically waits.
- Unstable network connection.

**Solution**:
```bash
# Increase concurrent coroutines (be cautious, too high may trigger risk control)
/workers 3

# Switch data center
/dc 1

# Increase cache size
/size 64M

# Check network connection and proxy settings
/proxy socks5://your-proxy:1080
```

### Issue 3: File Reference Expired Error (FILE_REFERENCE_EXPIRED)

**Symptom**: Errors occur midway when downloading large files.

**Cause**: Telegram file references expire automatically.

**Solution**: The program already handles this automatically; it will automatically refresh the reference and retry. If the issue persists:
```bash
# Restart the program to clear cache
docker restart tgfilebot
# Or if running locally, press Ctrl+C and restart
```

### Issue 4: API Interface Returns 401 Unauthorized

**Symptom**: 401 error returned when calling API interfaces.

**Cause**: Password authentication failed.

**Solution Steps**:
1. Ensure `password` in `config.json` is not empty.
2. Verify that the URL contains the correct authentication parameters (`key` or `hash`).
3. If using `hash` authentication, recalculate the hash value.

```python
# Python hash calculation example
import hashlib
uid = 123456789
password = "mypass"
hash_val = hashlib.md5(f"{uid}{password}".encode()).hexdigest()[:6]
# Use: ?hash={hash_val}
```

### Issue 5: Concurrent Search Timeout

**Symptom**: Returns a timeout when calling the `/search` interface.

**Cause**: Searching multiple channels exceeds the client request timeout.

**Solution**:
```bash
# Limit the number of search channels (specify a single channel via cname parameter)
/search?keywords=keyword&cname=@mychannel

# Or reduce search channels
/list channels  # View current channels
/del index      # Delete infrequently used channels
```

---

## Performance Optimization Suggestions

### 1. Concurrent Parameter Tuning

```json
{
  "workers": 2,     // Start testing from 2, gradually increase to 3-4
  "maxSize": 32M    // 32-64MB is a relatively balanced cache size
}
```

### 2. Proxy Configuration

If running in China or regions where Telegram is restricted:

```bash
/proxy socks5://proxy.example.com:1080
```

### 3. Data Center Selection

```bash
/dc 1   # China region
/dc 2   # Europe
/dc 4   # US
# Choose the nearest DC based on network latency
```

### 4. Cache Pre-warming

The message cache is built during the first run. It's recommended to:
- Pre-load message lists of frequently used channels.
- Wait for 1-2 hours for the LRU cache to stabilize.

---

## Security Advice

### ⚠️ Important Security Tips

1. **Password Management**
   - Do not hardcode passwords in the code; use environment variables.
   - Regularly change `password` and recalculate `hash`.
   - Use `hash` authentication instead of plaintext `key`.

2. **Sensitive Information Protection**
   - Do not share the `config.json` file.
   - Do not expose this service on the public network (use reverse proxy + HTTPS).
   - Do not log sensitive information such as API Tokens and phone numbers.

3. **Account Security**
   - Regularly check account login locations and activities.
   - Enable Telegram Two-Step Verification (2FA).
   - Do not run this program on untrusted code/environments.

4. **Network Security**
   ```bash
   # Use HTTPS reverse proxy (Nginx example)
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

## Notes

### Risk Control

- **Frequent Downloads**: Frequently using UserBot for large file downloads may touch Telegram API limits.
- **Suggested Config**: Set `workers` in a reasonable range (1-4, recommended 2).
- **Spaced Downloads**: Avoid continuous large downloads; give the account time to buffer.
- **Monitor Rate Limits**: Observe `FLOOD_WAIT` errors in program logs; if they appear frequently, reduce concurrency.

### Verification Code Input Limitation ⚠️ **IMPORTANT**

Due to Telegram's security policies, when submitting the `/code` verification code via a Bot message, Telegram servers will detect that the code is sent automatically (the bot is not a real person) and therefore reject it.

**Solution (Must be done manually)**:

1. Receive the 6-digit verification code sent by Telegram, e.g., `12345`
2. **Randomly insert any non-numeric characters** (letters, symbols, spaces all work) between the verification code digits.
   - Example: `12345` → `1a2b3c4d5` or `1-2-3-4-5` or `1 2 3 4 5`
3. **Manually** (no copy-pasting automated messages) send the obfuscated string to the Bot.
   - Command Example: `/code 1a2b3c4d5`
4. The program will automatically filter out all non-numeric characters and extract the real verification code `12345` to log in.

**Principle**: Telegram monitors the sending behavior of the "raw verification code string". By mixing in random characters, the message content no longer fully matches the verification code, bypassing this restriction.

---

## Differences Between Bot and UserBot & Risks

### Bot

- **Official Support**: Provided officially by Telegram, created and managed via @BotFather.
- **API Limits**: Functionality is limited by the TeleBot API, used for interacting with users, sending messages, managing groups, etc.
- **Security**: Bot accounts are relatively secure; they cannot log into the client like normal users, nor can they access users' private chat history.
- **Use Cases**: Public services, automated tasks, information pushing, etc.

### UserBot

- **Simulates User**: Achieves automated operations by simulating normal Telegram user behavior.
- **Permission Level**: Logs in using the user's API ID and API Hash, possessing the same permissions as a normal user.
- **Powerful Features**: Can perform all operations a normal user can (access private chats, send any type of file, join channels, etc.).

### Potential Risks

⚠️ **Before using UserBot, please fully understand the following risks**:

| Risk Type | Description | Mitigation Measures |
|:---|:---|:---|
| **Account Security** | Providing Telegram account credentials (API ID, API Hash, phone number) to the program; vulnerabilities or malicious use may lead to account theft. | Regularly view login records, enable 2FA, only run on trusted servers. |
| **Terms of Service** | Telegram's Terms of Service may prohibit or limit UserBot use; excessive abuse may result in account restrictions or even permanent bans. | Adhere to reasonable use policies, avoid frequent massive operations. |
| **Privacy Leakage** | UserBot can access all chat histories and contact information of the account. | Only run open-source code; audit it yourself or have someone trusted audit it. |
| **Stability** | UserBots are based on reverse engineering or unofficial APIs and are easily affected by Telegram updates. | Update the dependency library `gogram` promptly, follow community updates. |

**In this project, UserBot is primarily used for**:
- Obtaining direct links to media files that normal Bots cannot directly access.
- Accessing files in private channels.
- Performing high-performance stream transmission via the UserBot identity.

**Suggestions**:
- Use a dedicated Telegram account to run this program; do not use your daily account.
- Regularly back up session files (`files/*.session`) for emergencies.
- Enable Telegram account Two-Step Verification (2FA).
- Carefully check code changes; only use versions from trusted sources.

---

## License

This project is licensed under the [Apache 2.0](LICENSE) License.

---

## Changelog

### v1.1.6
- ✅ Fixed concurrent write out-of-order issues in message cache: transitioned from "lock-modify-then-publish immutable snapshot" to "**all writers perform copy-on-write and atomic swap under lock**", ensuring lock-free readers always observe a complete new or old snapshot, eliminating race conditions on underlying array modifications.
- ✅ Fixed crash during `/phone` login when users enter non-digit characters in verification codes (e.g., `1a2b3c`): added `stripNonDigits` helper to filter out non-numeric characters before `LoginByPhone`, ensuring only pure digit strings are passed to MTProto library.
- ✅ Improved parsing and formatting of multiple `t.me` links in `/link` command:
    - Removed default global deduplication to allow parsing **multiple distinct links** in a single message (e.g., `t.me/c/...` and `t.me/s/...` in the same album).
    - Call `parseTelLink` independently for each link and retain the first successful result, preventing subsequent failures from overriding previously parsed links.
    - Corrected `formatLink` logic to ensure each link independently builds parameters and sends requests, no longer relying on global hashes (`linkHashes`) to merge results.
- ✅ Fixed redundant processing of **private chat messages** during group management regex matching (`/parse group`, `admin`, etc.): added nil checks for `msg.MessageMediaWebPage` and `msg.MessageMediaDocument` to ensure regex matching only runs on group/channel media messages, preventing wasted computation and false positives.
- ✅ Optimized log cleanup (`/clean`) logic to support "keep only specified ID" or "delete only specified ID" operations based on `CleanRealm` parameter:
    - Retain only specified ID (`Keep != ""`): traverse files, compute hashes, and only delete files where `hash != Keep`.
    - Delete only specified ID (`Keep == ""`): traverse files, compute hashes, and only delete files where `hash == Keep`.
    - Fixed logic flaw where `Keep` and `Delete` parameters were conflated, ensuring filter logic strictly aligns with parameter intent.


### v1.1.5
- ✅ Fixed multi-link extraction keeping only the last result; each link is now resolved independently
- ✅ Stream downloads now abort with proper error logs when a chunk fails, instead of silently truncating the response per Content-Length
- ✅ Fixed a nil pointer crash during QR login for accounts with 2FA enabled; now guides users to `/phone` login instead
- ✅ Login flows are serialized with a lock, so concurrent `/phone` and `/qr` commands no longer start multiple logins at once
- ✅ Group management regex rules now only apply in groups/channels and no longer affect private chats
- ✅ API `limit` parameter is now capped at 100 (excess values are clamped), negative `offset`/`mid` are rejected
- ✅ HTTP Range requests whose start lies beyond EOF now return `416` per RFC 7233
- ✅ Message cache switched to lock-free immutable snapshots (copy-on-write), reducing allocations and lock contention under high concurrency
- ✅ With password auth enabled, channel posts without an individual sender no longer produce dead direct links (they are skipped with an explicit error instead)

### v1.1.4
- ✅ Enhanced concurrent download stability
- ✅ Optimized cache management strategies
- ✅ Improved error handling and logging
- ✅ Supported more search filtering options
- ✅ Optimized TCP disconnection detection and automatic recovery

### v1.0.0
- ✅ Initial release, supporting basic streaming downloads and Bot management

---

## Contribution Guidelines

PRs and Issues are welcome! Please ensure:
- Code complies with existing style
- Necessary tests are added
- Related documentation is updated
- Clear PR descriptions are provided

---

## FAQ

**Q: What video formats are supported?**  
A: Supports all video formats supported by Telegram (MP4, WebM, MKV, etc.), depending on the media type.

**Q: Can multiple people use it at the same time?**  
A: Yes, the program supports concurrent requests and multi-user authentication.

**Q: Is downloading files from private channels supported?**  
A: Yes, requires UserBot to be logged in and the `cate=user` parameter to be set.

**Q: Will the program save the downloaded files?**  
A: No, the program is only responsible for streaming forwarding and does not store any downloaded content.

**Q: How to monitor the program status?**  
A: Access the `GET /` endpoint, view the log file `files/log.log`, or use the `/info` command in the bot chat as an administrator or higher.

---

## Getting Help

- 📖 Check the troubleshooting section in this README
- 🐛 Submit an Issue to report problems
- 💬 Check closed Issues for similar problems
- 📝 Refer to comments and godoc in the project source code
