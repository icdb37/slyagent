# slyagent

## 开发工具

### Git Hook: 敏感数据检测

项目内置 `pre-commit` 钩子,在 `git commit` 时自动扫描暂存区文件,阻止包含
API Key、Secret、Password、Token、Private Key、数据库连接串等敏感数据
的提交。

**首次安装** (选择与你系统对应的脚本):

```bash
# Unix / macOS / Git Bash
./scripts/install-hooks.sh

# Windows PowerShell
powershell -ExecutionPolicy Bypass -File .\scripts\install-hooks.ps1
```

安装脚本会将 `core.hooksPath` 指向 `.githooks/` 目录,并对 hooks 文件赋予
可执行权限。

**日常使用**:

| 场景 | 命令 |
| --- | --- |
| 正常提交 (推荐) | `git commit -m "..."` |
| 跳过本次检测 | `git commit --no-verify -m "..."` |

**自定义检测规则**:

编辑 [.githooks/pre-commit](.githooks/pre-commit) 中的 `RULES` 数组即可新增
或删除检测规则;`SKIP_FILES` 数组配置跳过检测的文件模式;`PLACEHOLDER`
数组配置识别为占位符、可豁免的字符串(如 `your_key`、`example`、
`{{var}}`、`${var}` 等)。

**完整检测范围** (内置):

- AWS Access Key ID / Secret Access Key
- GitHub / GitLab Token
- 通用 API Key / Client Secret
- 密码赋值 (`password = "..."`)
- Auth / Access / Bearer / Refresh Token
- 私钥块 (`-----BEGIN PRIVATE KEY-----`)
- 数据库连接串带密码 (`mysql://user:pass@host`)
- JWT (`eyJ...`)
- Slack Token
- 长度 ≥ 32 的长随机密钥赋值

## LLM

### Anthropic
star-1.2k [sdk](https://github.com/anthropics/anthropic-sdk-go.git)


### Openai
star-3.5k [sdk](https://github.com/openai/openai-go.git)


### MiniMax
https://platform.minimax.cn/docs/api-reference

**请求**
```json
curl --request POST \
  --url https://api.minimax.cn/anthropic/v1/messages \
  --header 'Authorization: Bearer <token>' \
  --header 'Content-Type: application/json' \
  --data '
{
  "model": "MiniMax-M3.1-Flash-Preview",
  "messages": [
    {
      "role": "user",
      "content": "9.11 和 9.9 哪个更大？"
    }
  ],
  "max_tokens": 4096,
  "thinking": {
    "type": "adaptive"
  },
  "output_config": {
    "effort": "max"
  }
}
'
```


**应答（一次）**
```json
{
  "id": "066b367547c2650d17dc215f503da551",
  "type": "message",
  "role": "assistant",
  "model": "MiniMax-M3.1-Flash-Preview",
  "content": [
    {
      "thinking": "The user is asking which is larger: 9.11 or 9.9.\n\nComparing 9.11 and 9.9:\n9.9 = 9.90\n9.11 = 9.11\n\n9.90 > 9.11, so 9.9 is larger.",
      "signature": "7564f4e0e54b5c08b380d0b800aeb5463ea050e546cec7722e08ef6e912f6c67",
      "type": "thinking"
    },
    {
      "text": "**9.9 更大。**\n\n比较方法很简单：将两个数的小数位数对齐后比较：\n\n- 9.11 = 9.**11**\n- 9.9 = 9.**90**\n\n因为 90 > 11，所以 **9.9 > 9.11**。\n\n这是一个常见的思维陷阱——虽然 11 看起来比 9 大，但在比较小数时，应该先比较整数部分（都是 9），然后再比较小数部分，而小数部分需要**补齐位数**后再比较。",
      "type": "text"
    }
  ],
  "usage": {
    "input_tokens": 13,
    "output_tokens": 172,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 157
  },
  "stop_reason": "end_turn"
}
```

**应当（流式）**

SSE数据格式
```text
[event: 事件名]
[id: 事件ID]
[retry: 毫秒]
data: 数据
data: 多行数据
空行
```

按行读取数据，并且获取前缀`data: `之后的数据
```text
event: message_start
data: {"type":"message_start","message":{"id":"070d4551e7e5d3305343ac5cfb63e95d","type":"message","role":"assistant","content":[],"model":"MiniMax-M3","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0,"service_tier":"standard"},"service_tier":"standard"}}

event: ping
data: {"type":"ping"}
```
```json
[
    {
        "type": "message_start",
        "message": {
            "id": "070d4551e7e5d3305343ac5cfb63e95d",
            "type": "message",
            "role": "assistant",
            "content": [],
            "model": "MiniMax-M3",
            "stop_reason": null,
            "stop_sequence": null,
            "usage": {
                "input_tokens": 0,
                "output_tokens": 0,
                "service_tier": "standard"
            },
            "service_tier": "standard"
        }
    },
    {
        "type": "ping"
    },
    {
        "type": "content_block_start",
        "index": 0,
        "content_block": {
            "type": "thinking",
            "thinking": ""
        }
    },
    {
        "type": "content_block_delta",
        "index": 0,
        "delta": {
            "type": "thinking_delta",
            "thinking": "用户想了解 MySQL、PostgreSQL 和"
        }
    },
    {
        "type": "content_block_delta",
        "index": 0,
        "delta": {
            "type": "thinking_delta",
            "thinking": " Elasticsearch 之间的区别和使用场景。这是一个比较常见的数据库"
        }
    },
    {"":"......更多"},
    {
        "type": "content_block_delta",
        "index": 0,
        "delta": {
            "type": "signature_delta",
            "signature": "aeb67674b178ee8aacc5b16a0f58bde4eb4f355dafbbb76df7ca1ef066885793"
        }
    },
    {"":"......更多"},
    {
        "type": "content_block_stop",
        "index": 0
    },
    {
        "type": "content_block_start",
        "index": 1,
        "content_block": {
            "type": "text",
            "text": ""
        }
    },
    {
        "type": "content_block_delta",
        "index": 1,
        "delta": {
            "type": "text_delta",
            "text": "#"
        }
    },
    {
        "type": "content_block_delta",
        "index": 1,
        "delta": {
            "type": "text_delta",
            "text": " My"
        }
    },
    {"":"......更多"},
    {
        "type": "content_block_stop",
        "index": 1
    },
    {
        "type": "message_stop"
    }
]
```