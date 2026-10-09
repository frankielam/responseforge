# ResponseForge 使用说明

把接口响应收成表格。程序本身不读取 `.env` 文件。下面的值要先放进环境变量，再启动对应系统的程序。改完任何一项之后，关掉程序再开一次。

## 选哪个文件

| 系统 | 程序 |
|---|---|
| Windows AMD64 | `responseforge-windows-amd64.exe` |
| Windows ARM | `responseforge-windows-arm64.exe` |
| macOS Intel | `responseforge-macos-amd64` |
| macOS Apple 芯片 | `responseforge-macos-arm64` |

插件压缩包是 `responseforge-extension.zip`，四个系统用同一份。

## 第一次启动

1. 解压 `responseforge-extension.zip`。
2. 打开 Chrome 的 `chrome://extensions`，打开开发者模式，选择“加载已解压的扩展程序”，选中解压出来的 `chrome-mv3` 文件夹。
3. 在扩展选项里，服务器地址填 `http://127.0.0.1:8080`。访问令牌留空，除非你在下面设置了 `INGEST_TOKEN`。
4. 用下一节的方式启动服务。表格会出现在程序旁边的 `private/data` 文件夹里：`private/data/rules.json`、`private/data/raw.csv`、`private/data/tables/`。这个目录不会提交到开源仓库。

如果是从旧版本升级，请先把旧的 `data` 文件夹移动到 `private/data`；也可以通过 `DATA_DIR` 指定原来的位置。

## 修改配置

把下面的内容存成程序旁边的 `.env`，当作你自己的记录。启动时仍然要用后面的 Windows 或 macOS 命令把这些值设进环境变量。

```env
ADDR=:8080
STORAGE=csv
DATA_DIR=./private/data
INGEST_TOKEN=

MAX_BODY_BYTES=2097152

LLM_BASE_URL=
LLM_API_KEY=
LLM_MODEL=
LLM_TIMEOUT_SECONDS=90
LLM_MAX_SAMPLE_CHARS=12000
```

| 变量 | 作用 |
|---|---|
| `ADDR` | 监听地址。`:8080` 表示本机 8080 端口。 |
| `STORAGE` | `csv` 把结果写成文件。改成 `postgres` 时必须再填 `DATABASE_URL`。 |
| `DATA_DIR` | CSV 和规则的保存位置。默认是启动程序时所在文件夹下的 `private/data`。也可以填写固定位置，例如 `C:/responseforge/data`。 |
| `INGEST_TOKEN` | 留空则本机上传不需要令牌。填写后，扩展里的访问令牌必须是同一个值。 |
| `MAX_BODY_BYTES` | 服务端接受的最大响应体，单位是字节。 |
| `LLM_BASE_URL` | 模型接口地址。留空表示不调用模型，只保存原始响应。 |
| `LLM_API_KEY` | 模型接口的密钥。 |
| `LLM_MODEL` | 模型名称。地址不为空时，这一项必须填写。 |
| `LLM_TIMEOUT_SECONDS` | 等待模型的秒数。 |
| `LLM_MAX_SAMPLE_CHARS` | 发给模型的样例最多多少字符。 |
| `DEBUG` | 设为 `1` 才打印模型请求和响应正文。平时留空。 |

## 开启不同的模型

接口需要兼容 OpenAI 的 Chat Completions（地址以 `/v1` 结尾）。换模型时改这三项，然后重新启动：

```env
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=sk-your-key
LLM_MODEL=gpt-4o-mini
```

DeepSeek：

```env
LLM_BASE_URL=https://api.deepseek.com/v1
LLM_API_KEY=sk-your-key
LLM_MODEL=deepseek-chat
```

关闭模型：把 `LLM_BASE_URL` 留空。没有匹配规则时，响应仍会写入 `private/data/raw.csv`，只是不会自动生成解析规则。

## Windows

把 `start.bat.example` 复制成 `start.bat`，填上密钥，然后双击启动。

```bat
@echo off
set ADDR=:8080
set STORAGE=csv
set DATA_DIR=./private/data
set INGEST_TOKEN=
set LLM_BASE_URL=https://api.deepseek.com/v1
set LLM_API_KEY=sk-your-key
set LLM_MODEL=deepseek-chat
set LLM_TIMEOUT_SECONDS=90
set LLM_MAX_SAMPLE_CHARS=12000
set DEBUG=
responseforge-windows-amd64.exe
```

ARM 版 Windows 把最后一行换成 `responseforge-windows-arm64.exe`。

## macOS

在程序所在文件夹打开终端：

```bash
chmod +x responseforge-macos-arm64
export ADDR=:8080
export STORAGE=csv
export DATA_DIR=./private/data
export LLM_BASE_URL=https://api.deepseek.com/v1
export LLM_API_KEY=sk-your-key
export LLM_MODEL=deepseek-chat
export LLM_TIMEOUT_SECONDS=90
./responseforge-macos-arm64
```

Intel 的 Mac 把文件名换成 `responseforge-macos-amd64`。第一次打开如果系统拦截，到“系统设置 → 隐私与安全性”里允许该程序。
