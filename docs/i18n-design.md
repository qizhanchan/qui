# qui 国际化（i18n / l10n）设计

状态：设计草案，未实现。
读者：框架维护者。目标是给出一份可以直接分阶段动手的方案，而不是泛泛的"要支持多语言"。

---

## 1. 现状盘点

盘点的结论决定了工作量分布——**文字底层已经很完整，缺的是 locale 概念和布局镜像**。

### 已经有的（不需要重做）

| 能力 | 位置 | 说明 |
|---|---|---|
| Unicode shaping（GSUB/GPOS） | `text_shaping.go` | go-text/typesetting，真正的 OpenType 整形 |
| Bidi 段落方向解析 | `text_shaping.go:115 resolveTextDirection` | 按首个强方向字符判定，`TextDirectionAuto/LTR/RTL` |
| RTL 视觉序、caret、选区 | `text_caret.go`、`text_rich.go:105` | 连字 caret 位、RTL 选区分段都做了 |
| UAX #14 换行 / UAX #29 字素簇 | `text_shaping.go` | 中日韩换行、emoji 组合不会断错 |
| 逻辑对齐 | `text.go:64 TextAlignStart/End` | 已经是 start/end 而非 left/right，`inline.go:1026` 会按段落 RTL 翻转 |
| CJK 字体自动回退 | `font.go:294-298` | 换 Latin 字族不会让中日韩变豆腐块 |
| IME（组字/预编辑） | `ime.go` + `ime_darwin.m` | `NSTextInputClient` 完整对接 |
| Emoji 位图 | `emoji_darwin.m` | Core Text |
| PDF CJK 导出 | `pdf_cjk_test.go` | 子集化已覆盖 |
| `golang.org/x/text v0.38.0` | `go.mod` 直接依赖 | 已经在依赖里，`language`/`plural`/`number`/`currency`/`collate` **不需要新增依赖** |

### 缺的

1. **没有 locale 概念**。全仓库搜不到 `Locale` / `i18n` / `Translator`。
2. **没有翻译机制**。字符串在构造点写死（`widgets.NewButton("Save", ...)`）。retained-mode 下这是硬伤：即使有了字典，构造时解析的字符串在切语言时不会更新。
3. **没有 locale 敏感的格式化**。数字、日期、货币、相对时间、列表连接、排序（collation）全无。
4. **RTL 只到文字层，布局层完全没有**：
   - `Insets{Top, Right, Bottom, Left}`（`geometry.go:87`）是纯物理的，没有 inline-start/end；
   - `FlexLayout.Direction` 只有 `Horizontal/Vertical`，RTL 下主轴不会反向；
   - Grid 列序、ScrollView 水平原点/滚动条侧、Popup 锚定翻转、Dialog 按钮顺序、Slider 填充方向、返回箭头图标镜像——都没有。
5. **shaping 从不设置 `Language`**。`shaping.Input.Language` 字段存在但 qui 没填，导致汉字统一码的地区字形差异（`zh-Hans` / `zh-Hant` / `ja` 同一码位不同字形）拿不到正确字形，土耳其语 i、塞尔维亚斜体同理。
6. **htmlcss 不支持 `dir` / `lang` 属性，不支持 CSS `direction` 与逻辑属性**（`margin-inline-start` 等）。
7. **agent 选择器会被翻译打断**。`qui-agent click '[name="Save"]'` 一换语言就失效——这对"AI 原生可操作"这个定位是直接损伤。

### 框架自带的英文串很少（好消息）

只有寥寥几处：`dialog_darwin.go:95` 的默认 `"OK"`、`htmlcss/el.go:1280` 的 `"Submit"`/`"Reset"` 默认按钮名。**框架内建文案的翻译成本几乎为零**，绝大部分工作是给应用层提供机制。

---

## 2. 设计原则

1. **不破坏导入方向**。根 `qui` 永远不 import 子包。locale 的"种子"放根包，字典/复数/CLDR 格式化放 `i18n` 子包，两者通过根包定义的 `Translator` 接口连接——和现有的 `qui.Animator` / `qui.IMEClient` / `qui.VectorSource` 完全同构。
2. **不新增 import 边**。`widgets` / `htmlcss` / `reactive` 都只通过根包的 `qui.Translate` 拿译文，谁都不 import `i18n`。这保住了 CLAUDE.md 里"`reactive` 只 import 根包"的硬约束。
3. **retained-mode 正确性优先**：翻译必须在 **Draw/restyle 时**解析，不是构造时。这是本设计里最重要的一条，决定了 API 形状。
4. **复用现有失效机制**。locale 变更走 `SetTheme` 那条路：bump 一个 generation → invalidate 订阅窗口。文字测量缓存的 key 里加 locale gen，和现有的 `FontRegistryGeneration()` 一样。
5. **RTL 镜像在样式解析期完成**：逻辑属性 → 物理属性只发生一次，布局/绘制/命中测试下游代码完全不用改。浏览器就是这么做的。
6. **可退出**。q-excel 的网格、q-ppt 的画布这类天然 LTR 的内容，要能在子树上关掉镜像。

---

## 3. 分层架构

```
┌─────────────────────────────────────────────────────────────┐
│ cmd/qui-i18n     extract / lint / pseudo   （构建期工具）      │
├─────────────────────────────────────────────────────────────┤
│ i18n 子包        Bundle · T/TN · CLDR 复数 · 数字/日期/货币    │
│                  只 import 根 qui + golang.org/x/text        │
├──────────────────────── qui.Translator ─────────────────────┤
│ 根 qui (locale.go)  Locale · Direction · Translate ·         │
│                     LocaleGeneration · Window.SetLocale      │
├─────────────────────────────────────────────────────────────┤
│ widgets / htmlcss / reactive   —— 只调根包，互不新增依赖        │
└─────────────────────────────────────────────────────────────┘
```

---

## 4. 根包接缝：`locale.go`

放根包的理由：`Direction()` 要喂给布局引擎和文字基方向，是引擎级概念；`Translator` 是接口，不引入实现依赖。

```go
package qui

// Locale 是 BCP-47 标签："en" / "zh-Hans" / "zh-Hant-TW" / "ar-EG"。
// 零值表示"跟随系统"。
type Locale string

func (l Locale) Language() string       // "zh-Hans-CN" -> "zh"
func (l Locale) Script() string         // -> "Hans"
func (l Locale) Region() string         // -> "CN"
func (l Locale) Direction() TextDirection // ar/he/fa/ur/yi/ckb/dv -> RTL
func (l Locale) Fallbacks() []Locale    // zh-Hant-TW -> [zh-Hant-TW, zh-Hant, zh]

// SystemLocale 读取操作系统首选语言（平台桥接：locale_darwin.go /
// locale_other.go，与 dialog/ime 同一套 build tag 拆分）。
func SystemLocale() Locale

// ---- 翻译接缝（i18n 子包实现，根包永不 import 它）----

type Translator interface {
    // Translate 返回译文与是否命中。未命中时根包回退到 key 本身。
    Translate(loc Locale, key string, args map[string]any) (string, bool)
    // TranslatePlural 按 CLDR 复数类别选择。
    TranslatePlural(loc Locale, key string, n float64, args map[string]any) (string, bool)
}

func SetTranslator(t Translator)
func Translate(loc Locale, key string, args map[string]any) string

// ---- 全局 / 每窗口 locale ----

func SetDefaultLocale(l Locale)  // 失效所有订阅窗口 + bump generation
func DefaultLocale() Locale

// LocaleGeneration 每次 locale 或 translator 变更时递增。
// 任何缓存了已解析文案/已整形文本的地方都要把它放进 cache key，
// 语义与用法同 FontRegistryGeneration()。
func LocaleGeneration() uint64

func (w *Window) SetLocale(l Locale)  // 每窗口覆盖（多窗口多语言）
func (w *Window) Locale() Locale      // 未设置则回落 DefaultLocale()

func OnLocaleChange(fn func(Locale)) (cancel func())
```

### 为什么必须有 `LocaleGeneration`

`text.go` 的 `BuildTextLayout` 记忆化缓存目前的 key 不含 locale。一旦 shaping 开始带 `Language`（见 §9），同一串 `"骨"` 在 `zh-Hans` 和 `ja` 下字形不同、宽度可能不同。不进 key 就是脏缓存。`widgets.Label` 的富文本布局缓存、`InlineBox` 的段落缓存同理（CLAUDE.md 已经要求它们把 `FontRegistryGeneration()` 放进 key，照抄即可）。

---

## 5. `i18n` 子包

```go
package i18n // import "github.com/qizhanchan/qui/i18n"

// ---- 装载 ----
//go:embed locales/*.json
var builtin embed.FS

func Load(fsys fs.FS, dir string) error   // 扫描 dir/*.json，文件名即 locale
func Register(loc qui.Locale, msgs map[string]Message)
func Install()                             // qui.SetTranslator(defaultBundle)

// ---- 查询（用 qui.DefaultLocale()）----
func T(key string, args ...any) string           // T("file.save")
                                                  // T("greet", "name", user)
func TN(key string, n int, args ...any) string   // CLDR 复数

// ---- 显式 locale / 格式化 ----
func In(loc qui.Locale) *Printer

type Printer struct{ /* ... */ }
func (p *Printer) T(key string, args ...any) string
func (p *Printer) TN(key string, n int, args ...any) string
func (p *Printer) Number(v any, opt ...NumberOption) string
func (p *Printer) Currency(v any, code string) string
func (p *Printer) Percent(v float64) string
func (p *Printer) Date(t time.Time, style DateStyle) string   // Short/Medium/Long/Full
func (p *Printer) Time(t time.Time, style DateStyle) string
func (p *Printer) RelativeTime(t time.Time, now time.Time) string // "3 天前"
func (p *Printer) List(items []string, style ListStyle) string    // "A、B 和 C"
func (p *Printer) Collator() *collate.Collator  // 供 TableView 排序
```

底层全部用 `golang.org/x/text`（`language.Matcher` 做协商、`feature/plural` 做复数、`number` / `currency` 做数字、`collate` 做排序）。**不引入新依赖，也不自己实现 ICU**。

### 词条格式

`locales/zh-Hans.json`：

```json
{
  "file.save": "保存",
  "greet": "你好，{name}",
  "list.count": { "one": "{n} 个项目", "other": "{n} 个项目" },
  "@meta": { "locale": "zh-Hans", "fallback": "en" }
}
```

- 占位符用 `{name}`，命名而非位置——译者不能靠顺序，语序会变。
- 复数用 CLDR 类别名（`zero/one/two/few/many/other`），由 `x/text/feature/plural` 判定。阿拉伯语 6 类、俄语 4 类、中文 1 类，硬编码 `if n == 1` 是错的。
- `en.json` 是**源语言、唯一真相**，由 `qui-i18n extract` 生成，其余语言从它派生。

### 未命中策略

`locale → 语言回退链（zh-Hant-TW → zh-Hant → zh）→ 默认 locale → key 原文`。
开发模式（`QUI_I18N_STRICT=1`）下未命中返回 `⟦key⟧` 并 log 一次，让缺词条在界面上刺眼可见；生产模式静默回落。

---

## 6. widgets 层：retained-mode 的关键决策

**问题**：`widgets.NewButton(i18n.T("file.save"), fn)` 会把译文冻结在构造点，切语言不更新。

**方案**：给带文案的控件加一个 key 字段，`Draw` 期解析。

```go
// widgets/button.go
type Button struct {
    Text    string  // 字面量（不翻译）
    TextKey string  // 非空时，Draw 期用 qui.Translate 解析，覆盖 Text
    TextArgs map[string]any
}
func (b *Button) SetTextKey(key string, args ...any)
```

涉及：`Button`、`Label`、`CheckBox`、`RadioButton`、`Switch`、`Input.Placeholder`、`Select.Placeholder`、`MenuItem.Label`、`Dialog` 按钮、`TabView` 标签、`TableView` 列头、`Anchor`。

`Measure` 与 `Draw` 都要走同一个解析函数，且解析结果按 `LocaleGeneration()` 缓存，避免每帧走 map 查询。

### 框架内建文案

新建 `i18n/locales/qui.<locale>.json`，命名空间 `qui.*`（`qui.ok` / `qui.cancel` / `qui.submit` / `qui.reset` / `qui.copy` / `qui.paste` / `qui.selectAll` / `qui.noResults` …）。随框架 embed，首发覆盖 en / zh-Hans / zh-Hant / ja / ko / de / fr / es / pt / ru / ar / he。应用可以覆盖同名 key。

`dialog_darwin.go:95` 的 `"OK"`、`htmlcss/el.go:1280` 的 `"Submit"`/`"Reset"` 改为读这套。

---

## 7. htmlcss 层

1. **属性**：`dir="ltr|rtl|auto"`、`lang="zh-Hans"` 参与继承，落到 `ComputedStyle.Direction` / `.Lang`。
2. **CSS 属性**：`direction`、`unicode-bidi`，以及逻辑盒模型
   `margin-inline-start/end`、`margin-block-start/end`、`padding-inline-*`、`border-inline-*`、`inset-inline-*`。
   按 CLAUDE.md 的约定实现在 `apply.go` 的共享 applier 里，`Render` 和 `El` 两条路一起生效。
3. **选择器**：`:dir(rtl)`、`:lang(zh)`。
4. **静态标注**：`data-i18n="file.save"`、`data-i18n-placeholder="search.hint"`，在 restyle 期解析。
   `engine_live.go` 的合并刷新里把 `LocaleGeneration()` 也作为触发源——**locale 变更 = 一次全文档 restyle**（等价于 mount 时的 `Restyle()`）。这条路已经存在，成本可控。

```html
<button data-i18n="file.save">Save</button>
<input data-i18n-placeholder="search.hint" dir="auto">
```

---

## 8. reactive 层：走信号路径

locale 是典型的"低频但影响全局"的值。但**全量重渲染是浪费**——文案变化只需换 widget 的字符串。

```go
// i18n/reactive_bridge.go —— 只 import 根 qui，通过 HostData 交付，
// 不产生 i18n -> reactive 的 import 边。
```

实际做法：`reactive/html` 提供

```go
h.T("file.save")                    // 绑定文本节点，locale 变 -> 直接调 setter
h.T("greet", "name", userName)
h.TN("list.count", n)
h.Input().PlaceholderKey("search.hint")
```

`h.T` 下沉到已有的 `BindText`——**信号路径，不触发 reconcile**，和框架"signals skip the render pass"的哲学一致。locale 信号由 `h.Mount` 时挂到 `Runtime.SetHostData`（和 StyleEngine 同一套机制，每窗口一份，多窗口不串味）。

---

## 9. shaping 带 language

`shaping.Input.Language` 目前恒为空。改为从生效 locale 填入：

- 汉字地区字形（`zh-Hans` / `zh-Hant` / `ja` / `ko` 同码位不同字形）→ OpenType `locl` feature 生效；
- 土耳其语/阿塞拜疆语 `i`/`İ`、塞尔维亚西里尔斜体变体同理。

**实测校正（重要，别高估这一步的收益）**：语言标记只是**前提**，不是结果。
在 macOS 上实际验证过——qui 自动探测到的系统 CJK 字体（Hiragino Sans GB）
`GSUB` 里**根本没有 `locl` feature**，script list 里也没有注册任何 langsys：

```
GSUB features: [aalt dlig fwid hwid nalt pwid trad vert vrt2]
has locl: False
scripts/langsys: [(DFLT []) (cyrl []) (grek []) (hani []) (kana []) (latn [])]
```

所以 `zh-Hans` / `zh-Hant` / `ja` 三个 locale 下同一串汉字**渲染出来逐像素相同**
（examples/i18n 里做了像素对比，差异为 0）。语言标记该传还是要传（浏览器就是这么做的，
且换成 Source Han Sans / Noto Sans CJK 全量版立刻生效），但真正解决"日文渲染成中文字形"
这个用户能感知的问题，靠的是**按 locale 选字体**，不是靠 `locl`。

因此 `SetFontFallbackForLocale`（见下）不是锦上添花，而是这一节的**主要交付物**——
当前尚未实现，列在 P3。

### 实测发现：系统 CJK 字体当主字体，西里尔字母是全角的

同一次验证里量出来的第二个问题，比 `locl` 严重得多，而且**先于本次 i18n 改动就存在**——
只是以前没有任何东西渲染西里尔字母，所以没人看见。

`font.go` 的惰性初始化会探测系统 CJK 字体（macOS 上是 Hiragino Sans GB）并把它装成
**主字体**。它确实覆盖 Latin + CJK + Cyrillic + Greek，但它是一个 CJK 字体——
西里尔字母走的是**全角**字形：

```
14px 下的实测 advance：
  "o" (Latin)     8.75      "о" (Cyrillic)  14.00   ← 正好等于字号 = 全角
  "n" (Latin)     8.66      "н" (Cyrillic)  14.00
  "$"             9.09      "₽" (U+20BD)    14.00   ← 豆腐块，该字重根本没这个码位
  bold "о"        8.55      bold "н"         8.45   ← 正常
```

粗体正常，是因为粗体走 `defaultWeightFonts`（内置 Go 字体，有正常比例的西里尔字母）；
**常规字重**走主字体，于是俄语正文每个字母之间都被撑开，看起来像加了 letter-spacing。
`examples/i18n` 切到 Русский 一眼就能看到。

根因是"CJK 字体当主字体"这个架构选择本身：它让中日韩开箱即用，代价是所有非 Latin
非 CJK 的文字都从一个 CJK face 取字形。正确的修法是**反过来**——主字体用比例
Latin/Cyrillic face，CJK 只进 per-glyph fallback 链——但这会改变每一个现有应用的
渲染结果，不能夹在别的 PR 里悄悄做。

连同上一节的结论：`SetFontFallbackForLocale` + 主字体策略反转，是 P3 里真正要做的事，
优先级应高于 `locl`。

配套：
- `qui.SetFontFallbackForLocale(lang string, faces ...)` —— 让 `ja` 优先 Noto Sans JP 而不是落到中文字族（"错语言字形"是 CJK 用户最常见的抱怨）。
- `LocaleGeneration()` 进文字布局缓存 key（见 §4）。

**已知缺口（明确不做，写进文档）**：泰语/高棉语/老挝语需要词典分词才能正确换行，UAX #14 不够。留作后续，或允许应用注入 `qui.SetLineBreaker(lang, fn)`。日语禁则处理（`line-break: strict/normal/loose`、行头行尾禁则字符）优先级高于泰语，可在 P3 做。

---

## 10. RTL 布局镜像（最大的一块工程）

### 10.1 方向的传播

`Style` 新增：

```go
type Style struct {
    // ...
    TextDirection TextDirection // Auto = 继承父节点；根节点回落 Window.Locale().Direction()
}
```

布局 pass 开始时自顶向下解析一次，缓存在 `BaseWidget.effectiveDir`。子树设 `TextDirectionLTR` 即可退出镜像（q-excel 网格、q-ppt 画布、代码编辑器用得上）。

### 10.2 逻辑 → 物理，只解析一次

```go
type LogicalInsets struct { BlockStart, BlockEnd, InlineStart, InlineEnd float32 }
func (li LogicalInsets) Resolve(dir TextDirection) Insets
```

`Style.PaddingLogical` / `MarginLogical` 在样式合并期解析成现有的物理 `Insets`。**`Insets` 保持物理定义不变**，绘制、命中测试、`InteractionTransformer` 等下游一行不改。

### 10.3 逐处改造清单

| 位置 | RTL 行为 |
|---|---|
| `layout.go` FlexLayout | `Horizontal` 主轴从右向左排；`AlignStart/End` 映射翻转；交叉轴不变 |
| `layout.go` GridLayout | 列序反转，`grid-column: 1` = 最右列 |
| `AbsolutePositionValue` | 增加 `InlineStart/InlineEnd`，解析成 Left/Right |
| `widgets/scrollview.go` | 水平滚动原点在右；竖直滚动条移到左侧 |
| `widgets/popup.go` / `menu.go` | 首选展开方向翻转，屏幕边缘 clamp 逻辑镜像 |
| `widgets/dialog.go` | 按钮顺序反转 |
| `widgets/slider.go` / `progress.go` | 填充方向反转 |
| `widgets/table.go` | 列顺序反转 |
| `widgets/tabview.go` | 标签顺序反转 |
| `widgets/image.go` + `qui.VectorSource` | 新增 `MirrorInRTL bool`（返回箭头、撤销图标需要镜像；播放键、logo 不能镜像） |
| `tooltip.go` | 锚定方向翻转 |
| 文字选区拖拽 | 已经在 `text_caret.go` 处理，验证即可 |

**建议顺序**：逻辑属性解析 → Flex → Grid → 各 widget → htmlcss 映射。前两步做完就能覆盖大部分真实界面。

---

## 11. AX / agent：让选择器扛得住翻译

这是 qui 特有、且**必须在第一阶段做**的一点。现在 `qui-agent click '[name="Save"]'` 一旦界面翻译成中文就失效，agent 脚本、CI 冒烟测试全断。

```go
// accessibility.go
type AXNode struct {
    // ...
    Name    string `json:"name,omitempty"`
    // NameKey 是产出 Name 的消息 key（若该节点文案来自词条）。
    // 它不随语言变化，是 agent 选择器的稳定锚点。
    NameKey string `json:"nameKey,omitempty"`
    Lang    string `json:"lang,omitempty"`
    Dir     string `json:"dir,omitempty"` // "ltr" | "rtl"
}
```

配套：
- `selector.go` 新增 `[key=qui.save]`（含 `*=` / `^=` / `$=` 变体）；
- `agent/llm.txt` 里说明：**跨语言脚本优先 `#id` 或 `[key=]`，`[name=]` 只在单语场景用**；
- 可选 `Named` 接口伴生一个 `NameKeyed` 接口，和现有 `Roled`/`Valued` 同构。

---

## 12. 工具链 `cmd/qui-i18n`

```bash
qui-i18n extract   # AST 扫描 i18n.T/TN、h.T、TextKey:、embed HTML 的 data-i18n
                   # -> locales/en.json（保留已有译文，只增删 key）
qui-i18n lint      # 缺失 key / 冗余 key / 占位符不匹配 / 复数类别缺失 /
                   # widget 构造函数里的硬编码非 ASCII 字面量
qui-i18n pseudo    # 生成伪本地化 locale
```

**伪本地化值得单独说**：生成 `[!!! Şàvé Ϝïℓé ЖЖЖЖ !!!]` 这种——加重音符号（验证字体回退）、拉长 40%（验证截断/裁切）、加包围标记（验证是否有串没走词条）。配合仓库已有的 `QUI_GOLDEN=1` / `QUI_HTMLCSS_SNAPSHOT=1` 快照基建，一次 CI 跑完就能把布局溢出全暴露出来。**这是投入产出比最高的一个工具。**

---

## 13. 测试策略

- **单测**：`i18n` 的回退链、复数类别、占位符插值、格式化输出。
- **布局测**：`qui.NewTestWindow` + `SetLocale("ar")`，断言 Flex 主轴坐标、Grid 列序、Popup 锚点。
- **快照测**：每个 htmlcss 快照用例扩三个 locale——`ar`（RTL）、`ja`（CJK 换行）、`de`（长词溢出）+ 伪本地化。
- **agent 测**：同一段脚本在 `en` / `ar` 下都要能跑通（用 `[key=]` 选择器）。
- **回归**：`go test ./htmlcss ./reactive/...`（CLAUDE.md 里 backing-primitives 契约的要求，改 widgets 文案字段必跑）。

---

## 14. 分阶段路线

### P0 —— 接缝与机制（无视觉变化，可独立合并）
- 根包 `locale.go` + `locale_darwin.go` / `locale_other.go`
- `i18n` 子包：Bundle / T / TN / 回退链 / Install
- `cmd/qui-i18n extract` + `lint`
- 框架内建 `qui.*` 词条（12 语言）+ `dialog.go`、`htmlcss/el.go` 改造
- **AX `NameKey` + `[key=]` 选择器**（越早越好，晚了 agent 脚本要重写）

### P1 —— 应用可用
- widgets 的 `TextKey` 字段族
- `htmlcss` 的 `data-i18n` / `lang` / `dir` 属性
- `reactive/html` 的 `h.T` / `h.TN`（信号路径）
- `i18n.Printer` 的数字/日期/货币/相对时间/列表
- shaping 带 `Language` + `LocaleGeneration` 进缓存 key
- `qui-i18n pseudo` + 快照测试接入 CI

### P2 —— RTL 镜像
- `Style.TextDirection` 传播 + `LogicalInsets` 解析
- FlexLayout / GridLayout / AbsolutePosition
- widgets 逐个（ScrollView / Popup / Dialog / Slider / Table / TabView / Image 镜像）
- htmlcss 的 CSS 逻辑属性 + `direction` + `:dir()`
- 阿拉伯语 golden 快照

### P3 —— 长尾
- 日语禁则（`line-break`）
- `TableView` 用 collator 排序（当前是字节序，中文按拼音排会错）
- 每 locale 字体优先级（`SetFontFallbackForLocale`）
- 日期/时间选择器控件（周首日、历法）
- 在 `apps/q-excel` / `q-word` 落地一遍，验证机制够用（CLAUDE.md 里 apps 的定位就是压测框架）
- 泰语/高棉语分词换行（或开放 `SetLineBreaker` 注入点）

---

## 15. 非目标

- 不实现完整 ICU MessageFormat（`{n, plural, ...}` 嵌套语法）。命名占位符 + CLDR 复数类别覆盖 95% 场景。
- 不做运行时机器翻译。
- 不翻译用户内容（文档正文、单元格数据）——那是应用的事。
- 不支持竖排（`writing-mode: vertical-rl`）。日语竖排排版是独立的大工程，需要时另开设计。
