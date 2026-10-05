# NewAPI 视频适配配置导入说明

## 简介

把上游视频接口文档整理成配置文件，一次导入模板与模型规则。仅根用户可以使用；导入和转换预览不生成视频、不扣费，也不自动发布。

## 如何使用

1. 在「设置 → 视频适配」下载「配置模板」和这份「填写说明」。模板是 SD2.5 CH1 的填写示例，不代表其他渠道或同名模型也采用这些参数。
2. 把这两个文件、目标模型名、上游完整文档（包括提交、查询、下载、响应示例）一起交给 AI。只有空模板，没有上游文档，AI 不能可靠判断字段。
3. 核对 AI 返回的 JSON，保存为 UTF-8 的 `.json` 文件；不要附带 Markdown 代码围栏或注释。文件最多 2 MiB。
4. 点击「导入配置」，上传文件或粘贴 JSON。选择当前站点的实际渠道；所选渠道会替代文件里每条规则的渠道编号。不同规则需要不同渠道时分批导入，或在文件中填写正确的 `channel_ids` 并不勾选重绑渠道。
5. 点击「校验并预览」，检查新增模型、渠道、字段和查询方式，再点击「加入草稿」。原有草稿保留，不覆盖同编号模板；启用规则冲突会报错，不会悄悄替换。
6. 在模型行使用「转换预览」和「响应解析预览」，确认素材结构和状态读取，再点击「发布规则」。发布前线上配置不变；校验通过仅说明配置符合当前规则，不代表真实上游一定可用。
7. 发布后按实际需要做一次生成验证。真实生成按渠道计费。

## 功能介绍

- 导入配置：新增模板与模型规则，可将导入规则统一绑定到选定渠道。
- 下载配置模板：提供可编辑 JSON 示例，查询和响应配置也包含在内。
- 下载填写说明：包含字段解释、操作步骤和可复制的 AI 提示词。
- 导出当前配置：导出当前页面草稿，包括尚未发布的更改；不包含渠道密钥及渠道地址。若曾在附加请求头或常量中手工填写秘密值，分享前自行删除。
- 导入只是追加，不是恢复整站或删除现有规则。导回同一渠道、同一模型的启用规则会提示冲突；修改已有规则请直接编辑原行。

## 文件结构

顶层只接受 `format`、`format_version`、`templates`、`rules`：

- `format` 固定为 `newapi-video-adapters`，`format_version` 固定为 `1`，不要填写线上发布版本。
- `templates`：每项包含唯一 `id`、展示名称 `name` 和完整 `protocol`。
- `rules`：每项包含唯一 `id`、`enabled`、`channel_ids`、`models`、`template_id`。可选 `override` 是整份协议，不是局部补丁。
- `models` 必须显式填写数组，填渠道映射后的实际上游模型名，不按名字猜协议。省略或 null 会被拒绝；空数组意味着渠道默认规则，请仅在明确需要时使用。
- 同一文件内模板编号和规则编号各自不能重复；`template_id` 必须引用文件内的模板。多个模型可写在同一 `models` 数组内共用模板。
- `rules: []` 可以只导入模板，之后在页面添加绑定。导入模型规则时，必须在文件或页面指定实际存在的渠道。
- 未识别的配置属性会拒绝导入，不会静默忽略拼错的字段。

## 提交与查询

`protocol` 按下载模板的完整结构填写：

| 字段 | 含义 |
| --- | --- |
| `enabled` | 布尔值，模板通常为 true；是否生效还取决于规则启停与发布 |
| `submit_path` | 相对提交路径，如 `/v1/videos`；当前提交方式为 POST |
| `poll_method` | 查询方式 GET 或 POST，POST 本身不等于异步 |
| `poll_path` | 相对查询路径，如 `/v1/videos/{id}`；GET 必须含 `{id}` |
| `poll_id_field` | POST 查询请求中的任务 ID 字段，GET 查询时不使用 |
| `content_path` | 相对下载路径，可留空以使用结果地址 |
| `encoding` | 提交格式 `json`、`form` 或 `multipart` |
| `auth_mode` | `header`、`query` 或 `none` |
| `auth_name` / `auth_prefix` | 鉴权字段及前缀，如 Authorization 和 `Bearer `（末尾有空格）；实际密钥来自渠道 |
| `headers` | 附加固定请求头，通常 `{}`；不要填密钥 |
| `defaults` | 附加固定字段，通常 `{}`；不能覆盖映射字段或固定生成时长 |

路径仅填写 `/` 开头的相对路径，上游地址和密钥留在渠道配置中。不能在文件里加入脚本、代码或自定义网络请求。

### 响应读取 response

- `id`：提交响应中的任务编号路径，如 `id`、`data.id`。
- `status`：查询响应中的状态路径，如 `status`。
- `url`：成品地址路径，多个候选用 `|` 分隔，例如 `url|video_url`。
- `error`：错误信息路径，如 `error.message`。
- `progress`：实际进度字段路径；文档未提供则 `""`，不要编造百分比。
- `states`：左边为上游状态，右边只能是 `queued`、`in_progress`、`completed`、`failed`。例如 `"processing": "in_progress"`。

查询配置也要按文档核对；配置正确后由系统自动查询，不需要用户手动访问上游。

## 字段映射 fields

每项至少包含 `source`、`target`、`format`。**source 是文运的统一输入字段，target 才是上游字段**。不要把两侧都改为上游命名。

| 统一 source | 用途 | 示例 target（以实际文档为准） |
| --- | --- | --- |
| `model` | 上游实际模型名 | `model` |
| `prompt` | 提示词 | `prompt` |
| `duration` | 已校验的时长（秒） | `duration` 或 `seconds` |
| `resolution` | 清晰度 | `resolution` |
| `aspect_ratio` | 宽高比 | `aspect_ratio` |
| `image_urls` | 参考图片数组 | `image_refs` |
| `video_urls` | 参考视频数组 | `video_refs` |
| `audio_urls` | 参考音频数组 | `audio_refs` |
| `first_frame` / `last_frame` | 独立首尾帧 | `first_image` / `last_image` |
| `generate_audio` | 是否生成音频，和参考音频不同 | `generate_audio` |
| `mode` | 生成模式 | 仅上游要求时映射 |
| `extra_parameters.KEY` | 已声明的自定义参数 | 上游对应字段或嵌套路径 |

`model`、`prompt`、`duration` 三项映射必需。其他参数仅按文档需要加入；传了素材或生成音频参数但没有对应映射时会报错，不会悄悄丢弃。

### 值格式与可选属性

| format | 行为 |
| --- | --- |
| `identity` | 保持原值；字符串数组仍是字符串数组 |
| `string` / `number` / `boolean` | 转字符串、数字、布尔 |
| `not` | 布尔取反 |
| `object` | 用 item_key 包装为一个对象 |
| `objects` | 数组每项包装为对象 |
| `single` | 只接收单元素数组并取该项，多项报错 |
| `single_object` | 只接收单元素数组并包装为对象 |
| `array` | 给整个值再包一层数组，不用于已经是数组的图片列表 |
| `frames` | 一至两项数组包装为 first_frame/last_frame 对象；不是独立首尾帧字段改名 |

- `item_key` 仅用于 object/objects/single_object。例如 `objects` + `image_url` 得到 `[{"image_url":"https://…"}]`。保持原值时无需填写。
- `scale` 仅用于数值单位换算，例如秒转毫秒填 1000；不换算时省略，字符串和数组不要填 1。时长只允许改名、转类型或换算单位，不能用常量、条件或枚举改变实际秒数。
- `values` 是枚举映射，例如 `{"720p":"HD"}`；没有对应项会报错。
- `fallback` 仅源字段缺失时补默认值；false 和 0 是有效值，不能替换成默认值。
- `when` 是全部满足才发送的条件数组：`{"source":"mode","operator":"eq","value":"references"}`。operator 仅为 exists/missing/eq/ne。条件 source 同样只能使用统一输入字段或已声明的 extra_parameters.KEY，不能填写上游字段名。
- target 和 item_key 可用点号表示嵌套字段，例如 `parameters.resolution`。禁止让目标字段重复或相互覆盖。

## 能力限制 capabilities

`duration` 对象可以填 min/max/default 或 values 枚举。resolutions、aspect_ratios、modes 用数组；模式只支持 text/references/frames。清晰度使用小写，例如 720p、1080p、4k。

image_limit、video_limit、audio_limit 是素材数量；0 表示禁止，省略表示文档未明确，未知不等于已验证支持。`media_transport: "url"` 表示上游需要可访问地址，内联或已上传素材会按既有素材流程处理；`either` 表示保留兼容来源。站点公开地址和素材访问配置仍须正确。

可选布尔能力：generate_audio（生成音频）、audio_requires_visual（音频需搭配视觉素材）、video_requires_image（视频需图片）、first_frame_with_video（允许首帧与参考视频）、prompt_optional_with_image（有图可省略提示词）。只根据文档填写，不根据示例或名字猜测。

`combinations` 保留每组合法组合，各项包含 duration、resolutions、aspect_ratios、modes；不能把不同条件合成一个并不存在的组合。

示例文档要求首帧和尾帧同时提供，并与多参考模式互斥，使用时应成对提供。现有校验会检查尾帧需首帧，但配置尚无“首帧必须搭配尾帧”的独立开关；导入不能凭空增加该约束，仍需核对上游要求。

### 新增自定义参数

在 capabilities.parameters 声明，随后在 fields 添加映射，例如：

```json
{
  "key": "camera",
  "label": "镜头方式",
  "type": "string",
  "editable": true,
  "required": false,
  "default": "static",
  "options": ["static", "moving"]
}
```

```json
{ "source": "extra_parameters.camera", "target": "options.camera", "format": "identity" }
```

type 仅支持 string/number/integer/boolean；数值可填 min/max，editable 控制是否显示给文运用户。生成音频要保留未指定、true、false 的区别。查询轮询、鉴权和计费不是给普通生成用户编辑的自定义参数。

## 给 AI 的提示词

```text
请根据附件的 NewAPI 导入模板、填写说明，以及我提供的上游接口文档，生成可导入的 JSON 配置。
目标模型名：<填写映射后的实际上游模型名>。渠道编号暂留空，我会在导入页面选择。
请保持 format 和 format_version，按文档填写提交、查询、下载、鉴权格式、响应解析、字段映射和能力限制。
source 必须使用统一字段，target 才使用上游命名；图片、视频、音频、首尾帧及生成音频分别处理。
不要猜测未声明能力，不把示例当作枚举限制，不擅自缩短时长、降低分辨率或丢弃素材。
如果有新参数，先声明 capabilities.parameters 再添加 extra_parameters.KEY 映射。不要包含密钥、密码、上游域名或脚本。
文档信息不足先问我；现有配置表达不了的流程或约束单独说明，不杜撰 JSON 属性。
最后返回完整 JSON 文件正文，不添加代码围栏、注释或额外说明字段。
```

已有的字段改名、类型转换、枚举、包装、单位换算和响应读取可以靠配置完成；新的签名算法、多阶段工作流等当前功能未覆盖的协议仍可能需要开发。AI 帮忙填写不等于自动发现或验证任意渠道。
