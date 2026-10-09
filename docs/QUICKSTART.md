# ResponseForge 快速开始

这份文档面向第一次使用、没有编程经验的用户。最简单的方式是：用 Docker 启动本地 Server，再构建并加载浏览器扩展。

## 需要准备什么

- Chrome 或 Edge；
- Docker Desktop；
- Node.js 和 npm，用来构建浏览器扩展；
- 一个你自己有权限访问的网站或后台。

如果电脑没有 Docker，可以使用后面的 Go 本地运行方式。Cloudflare Worker、PostgreSQL 和 LLM 都不是第一次运行所必需的。

## 1. 下载项目

让 AI Agent 下载或克隆 ResponseForge，然后进入项目根目录，也就是包含 `README.md`、`go.mod` 和 `extension/` 的目录。

让 Agent 先阅读根目录的 [`AGENTS.md`](../AGENTS.md)，再执行本文件的步骤。不要把 `.env`、Token、Cookie 或抓取到的响应内容发给 Agent。

## 2. 启动本地 Server

在项目根目录执行：

```bash
docker compose up -d server
```

确认 Server 正常运行：

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

能收到成功响应就表示 Server 已启动。默认使用 CSV 存储，不需要数据库和 LLM。

数据会保存在：

```text
private/data/
```

其中包括 `raw.csv`、`rules.json` 和 `tables/`。这些数据不会提交到开源仓库。

## 3. 构建浏览器扩展

在项目根目录执行：

```bash
cd extension
npm ci
npm run build
```

构建成功后，扩展目录是：

```text
extension/.output/chrome-mv3
```

如果提示没有 `npm`，请先安装 Node.js LTS，然后重新执行上面的命令。

## 4. 加载扩展

1. 打开 Chrome 或 Edge。
2. 访问 `chrome://extensions`。
3. 打开右上角的“开发者模式”。
4. 点击“加载已解压的扩展程序”。
5. 选择项目中的 `extension/.output/chrome-mv3` 文件夹。

AI Agent 可以帮你构建文件，但普通 Agent 不一定能够替你操作浏览器设置页面；如果它没有浏览器控制能力，需要你手动完成这一步。

## 5. 配置扩展

打开 ResponseForge 的扩展选项：

- Server URL：`http://127.0.0.1:8080`
- Access token：第一次本地运行留空
- Capture：开启
- URL pattern：填写你有权限访问的网站接口模式，例如：

  ```text
  https://example.com/api/orders*
  ```

先不要开启 LLM，也不要把真实 API Key 填入浏览器扩展。保存后重新打开或刷新目标页面。

## 6. 查看结果

访问目标网站并执行会触发接口请求的操作，例如打开列表、翻页或搜索。结果会写入：

- 没有匹配规则时：`private/data/raw.csv`；
- 有匹配规则时：`private/data/tables/` 下的 CSV 文件；
- 规则记录：`private/data/rules.json`。

扩展只捕获命中 URL pattern 的 `fetch` 和 `XMLHttpRequest` 响应，不会自动抓取所有页面内容。必须使用你有权限访问的数据。

## 7. 停止服务

```bash
docker compose down
```

停止容器不会删除 `private/data` 中已经保存的数据。

## 不使用 Docker 的方式

如果已经安装 Go，可以在项目根目录执行：

```bash
go run ./cmd/server
```

然后按照上面的步骤构建和加载扩展。默认仍然使用 `private/data` 和 CSV 存储。

## 常见问题

### `curl` 无法连接

检查 Docker 是否正在运行，并查看日志：

```bash
docker compose logs server
```

如果 8080 端口已被占用，可以设置其他端口映射，例如 `8081:8080`，然后把扩展里的 Server URL 改成 `http://127.0.0.1:8081`。

### 扩展没有数据

确认扩展已启用、URL pattern 能匹配当前请求，并刷新目标页面。ResponseForge 不捕获页面跳转、图片、视频、WebSocket，也不捕获没有经过 `fetch` 或 `XMLHttpRequest` 的请求。

### 没有生成结构化表

这是正常的：第一次运行没有规则时，响应会先写入 `private/data/raw.csv`。可以手工保存规则，或者在明确了解数据会发送到哪里之后再配置 LLM。

### 想使用 PostgreSQL、Cloudflare 或 LLM

这些是进阶部署方式。先阅读 [`README.md`](../README.md) 和 [`docs/USAGE.md`](USAGE.md)，并让 AI Agent 在执行远程部署、数据库迁移或配置模型密钥前明确说明会产生的外部影响。
