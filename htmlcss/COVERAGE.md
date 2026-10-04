# htmlcss — 能力矩阵与开发路线图 (Coverage & Roadmap)

这是 `htmlcss` 引擎的**唯一权威能力清单**，也是稳步推进开发的脚手架。
它回答三个问题：**支持什么、明确不支持什么（走原生 widgets）、下一步做什么**。

> qui 不是一个完整的浏览器。目标是「**轻量 HTML5 + CSS 子集**」，覆盖
> 应用开发中真正高频的排版/样式/交互，其余能力（媒体、画布、表格布局等）
> 建议直接用原生 `widgets`。本文件划清这条边界。

## 如何使用本文件（脚手架约定）

1. **本文件是状态的真相来源**。任何新增/修改标签或 CSS 特性的改动，都必须
   同步更新对应的矩阵行（改状态、移动到「已完成」）。
2. **每个 ✅ / 🟡 都应有测试背书**。新增特性时：
   - 逻辑正确性 → `*_test.go` 引擎测试（cascade/几何/事件），见文末「测试约定」。
   - 视觉效果 → snapshot（`QUI_HTMLCSS_SNAPSHOT=1`）或 `widgets/html_golden_test.go`。
   - 双路径（Render 静态 + reactive 实时）都要能过 —— appliers 是共享自由函数，
     测一条通常两条都覆盖，但涉及 restyle/事件的要在 reactive 侧也验一遍。
3. **每个特性必须在 [`examples/html-css`](../examples/html-css/) 里有可见的呈现内容**，
   方便人工验收。约定：
   - 纯渲染/静态特性 → 在 `page.html` + `style.css` 里加一段展示区块（带中/英文小标题说明用到的特性）。
   - 交互特性（`:hover`/`:focus`、点击、运行时 `display:none` 切换等）→ 同样放进 `page.html`；
     需要事件回调的用 `RenderDoc` 拿到元素句柄，在 `main.go` 里 `SetOnClick`/`SetClass` 接线
     （`Render` 结果是**活的可重样式化 El 树**，见 `main.go` 里的 display:none toggle 示例）。
   - 用 `go run ./examples/html-css` 验收；`-raw` 看无 CSS 的默认样貌。
4. **优先级队列（§5）是待办清单**。做完一项就从队列删除并把矩阵行改为 ✅/🟡。
5. **§4「缺口清单」是反向清单**（以浏览器原生能力为基准列「还没有的」），
   矩阵只列「已经想到的」，两者互补：新增支持时从 §4 删行 → 往 §1/§2/§3 加行 → 在 §6 登记测试。

**图例**：✅ 完整　🟡 部分（见备注）　❌ 尚未实现（在路线内）　🚫 设计上不支持（走原生）

---

## 1. HTML 标签

### 结构 / 文本 / 语义
| 标签 | 状态 | 备注 |
|---|---|---|
| `html` `head` `body` | ✅ | head 及其内容不渲染；body 为根 |
| `div` `section` `article` `header` `footer` `nav` `main` `aside` | ✅ | 通用 block 容器 |
| `span` | ✅ | inline，折叠进文本 run |
| `p` `h1`–`h6` | ✅ | 带 UA 默认 margin/font-size |
| `strong` `b` `em` `i` `small` `u` `s` `del` `ins` `strike` `mark` `abbr` `sub` `sup` | ✅ | inline，折叠为带样式 span |
| `code` `pre` | ✅ | monospace；`<pre>`/`white-space:pre` 保留空白+换行（见 white-space 行） |
| `a` | ✅ | href 点击 `OpenURL`；折叠 span 里也可点（`InlineBox.LinkAt`） |
| `br` | ✅ | inline 强制换行 |
| `blockquote` | ✅ | UA 左边框 + 缩进 |
| `hr` | ✅ | 渲染为 `widgets.Rule`；border-* 作用于线本身 |
| `ul` `ol` `li` | ✅ | `[marker\|content]` 悬挂行；disc/circle/square/decimal；ol 自动编号；剪贴板可重建 |
| `img` | 🟡 | 本地 raster + `.svg`（tint）+ `data:` URI + **`object-fit: cover/contain`**；**不支持** 远程 URL / `srcset` |
| `label` | 🟡 | 折叠为 inline 文本；**未**实现 `for=` 点击联动控件 |
| `table` `thead` `tbody` `tfoot` `tr` `td` `th` `caption` `colgroup` `col` | ✅ | 拍平成一个 `GridLayout`：列跨行对齐、内容自适应列宽（有 width 时等分 1fr）、`colspan`、`th` 粗体居中、`tr:nth-child` 行条纹、`border-spacing`、auto-width 表 shrink-to-fit。**Phase 2**：`<caption>` 全宽跨列行（`caption-side:top/bottom`，剪贴板行号自动去偏移）、`<colgroup>/<col>` 列宽（`width` 属性或内联 `style`→`GridFixedTrack`，支持 `span`、% 参照表宽）、`table-layout:fixed`（等分列、忽略内容宽）、`border-collapse:collapse`（单线合并：spacing 归零 + 每格只画 top+left、边缘格补 outer right/bottom，root `GridLayout` 无关）、`rowspan` 行高计入（root `GridLayout` 跨行 auto 分摊，见 `growAutoRowsForRowSpans`）。**限制**：`<col>` 不吃 class 选择器（列在自身级联前解析）；内容比例分配列宽未做 |
| `dl` `dt` `dd` `figure` `figcaption` | ✅ | UA 默认样式：dd/figure 缩进、dt 粗体、figcaption 弱化 |
| `script` `style` `meta` `link` `title` | 🚫→✅ | 不渲染；`<style>` 内容会被采集进样式表 |

### 表单控件（经 backing widget 渲染）
| 标签 | 状态 | 备注 |
|---|---|---|
| `input[type=text]`（默认） | ✅ | `widgets.Input`，placeholder/value/onInput/onSubmit |
| `input[type=checkbox]` | ✅ | `widgets.CheckBox`，onToggle；可选 value 标签 |
| `input[type=checkbox switch]` | ✅ | Safari 式 `switch` 布尔属性 → `widgets.Switch`（checkbox 语义：checked/:checked/表单序列化一致，`accent-color` 染 on 轨道）；`h.Switch()` |
| `input[type=radio]` | ✅ | 真 `RadioButton`，按 `name` 分组单选；`value`=可见标签，`checked` 初选 |
| `input[type=password]` | ✅ | `widgets.Input.Password` 掩码显示(• 每字符)、真值保留、caret/选区按掩码测量 |
| `input[type=range]` | ✅ | `widgets.Slider`，min/max/value/step；onChange 回传数值字符串 |
| `input[type=number/email/search/tel/url]` | 🟡 | 渲染为普通文本框(视觉/编辑等同 text)。**`type=search` 有 UA 清空按钮**（2026-08-13）：有内容时右侧内嵌 ✕，点击清空并触发 input、不触发 submit；走新的通用 `widgets.Input.TrailingButton` 内嵌附件机制（number 步进器将来可复用）。**`type=number` 有步进器**（2026-08-13）：右侧上下箭头，按 `step`（缺省 1）增减、clamp 进 `[min,max]`、空值/非数字从区间下限起步，走 onInput 上报。widget 只报方向（`Input.OnStep`），`step/min/max` 的 HTML 语义留在 htmlcss（`El.stepNumber`）。**欠**：类型校验（`:valid`/`:invalid` 那批） |
| `readonly` / `maxlength` 属性 | ✅ | 经 backing widget 的 `BeforeInput` 否决钩子实现（`El.installEditGuards`）——每条改动路径（type/delete/cut/paste/IME/InsertText/Enter）都过这个闸，所以**无需 widget 侧改动**，且 readonly 字段仍可聚焦/选中/复制（同浏览器）。`maxlength` 按 rune 计数、删除永不拦、替换选区可释放其长度；缺失/非法值=无上限。闭包读 `e.attrs`，运行时改属性即时生效 |
| `input[type=date/time/datetime-local/month/week]` | 🟡 | 文本框 + **格式提示 placeholder**(date→`YYYY-MM-DD`、time→`HH:MM` 等，author placeholder 优先，`typeFormatHint`)；**无**原生日期选择器/校验(qui 无 NSDatePicker 桥) |
| `input[type=color]` | 🟡 | 渲染为**填充色块按钮**(非文本框)：色块背景=当前值、点击弹**应用内色板 Popup**(Google-Docs 风：None 行 + 灰阶行 + 10 色相矩阵 tint→shade + 底部**手写 hex 输入带实时预览圆点**，圆形 swatch，`colorPaletteContent` 纯 Go 跨平台；swatch 点击或 hex 回车回填并 onInput；**弹出位置自适应** `anchoredPopupPos`——默认下方、下方放不下且上方够则翻上、都放不下则贴视口边保证全显，X 越界 clamp)；默认 48×26。**交互态仅改边框**：色块本体 + 调色板里每个 swatch，背景=所选值，hover/active/focus **不改背景/前景**（否则误导用户），只加边框环（色块本体经 `Box.Hover/Active` 只设 Border；调色板 swatch 因 `widgets.NewButton` 默认 states 会 darken 背景，故重建 `States.Base/Hover/Pressed/Focused` 同 fill 只改 Border + 清 `StateLayerColor`）。**无**原生取色面板/自由取色(qui 无 NSColorPanel 桥)。2026-08-13 修：表单序列化此前返回**初始** `value`（用户选的颜色提交不上去），reset 也不还原色块 |
| `input[type=submit/reset/button]` | ✅ | 渲染为**按钮**(非可编辑文本框)：走 Box+文字面(label = `value` 属性，缺省 Submit/Reset)、借 `<button>` UA chrome + 内建 hover/press；submit 点击提交上级 `<form>`、reset 恢复各控件默认(`resetForm`)、button 仅 author onClick。表单序列化里不算成功控件 |
| `input[type=file]` | ✅ | 渲染为**按钮**(借 `<button>` chrome)：点击弹**原生文件对话框**(`qui.OpenFile`/`OpenFiles`，`multiple` 属性走多选、`accept` 的 `.ext` 令牌→`AllowedExtensions`)；label=所选文件基名(多选加 `(+N)`)、缺省 `Choose File…`；表单序列化回传所选绝对路径。非 darwin 平台原生对话框返回 `ErrDialogNotSupported`(点击 no-op) |
| `textarea` | ✅ | `widgets.TextArea` |
| `select` `option` `optgroup` | ✅ | 需 `Options.Window`；无 window 时降级为占位 label。**option 语义完整**（2026-08-13）：`value` 是**提交值**（缺省回退到可见文字，HTML 规则；此前一直提交可见文字——表单语义硬伤）、`selected` 设初选并被 `<form>` reset 还原、`disabled` 灰掉且点击/方向键/type-ahead 都不会落上去（root 侧 `widgets.Select.ItemDisabled`）、`<optgroup label>` 在其选项前插一条**不可点的分组标题行**。**键盘 type-ahead**：聚焦后敲字母跳到匹配项、连敲同一字母循环、1s 无输入清空缓冲、前缀无匹配则回退到「只按最后一个字母搜」（时间取事件自带 `When`，测试可复现）。**限制**：分组是「标题行 + 平铺」的近似（无真嵌套/成员缩进）；无 `multiple`、`size`；`h` DSL 的 `.Options()` 只传标签（值=标签），需要独立值用 `El.SetSelectValues` / `h` DSL `.OptionValues(...)` / `.OptionDisabled(...)`（2026-08-13 补） |
| `button` | ✅ | Box 渲染 + 内建 hover/press 反馈 |
| `form` | ✅ | `El.SetOnFormSubmit(func(map[string]string))`：提交时收集**具名后代控件**的当前值（text/password/…/range/checkbox(勾选)/radio(选中)/textarea/select），未勾选/无 name 的省略（对齐 HTML 序列化）。触发：文本框回车、或默认/`type=submit` 的 `<button>`/`<input type=submit>` 点击（`enclosingForm` 上溯 + 一次性包裹 onClick）；`<input type=reset>` 恢复默认值。限制：无原生 action/method 网络提交、无 `formdata`/校验 |
| `fieldset` `legend` | ✅ | UA 边框/内距 + legend 粗体（无缺口式 legend 叠边框） |
| `label[for]` | ✅ | 点击关联控件：勾选 checkbox / 选中 radio / 聚焦文本框（`findByID`+`activateFromLabel`）。作为控件面：其文字**不可拖选**（`textLabel`/`InlineBox.Selectable=false`），点击只 toggle 不进选择模式 |
| `datalist` | ✅ | **原生补全**（2026-08-13，`datalist.go`）：`<input list=ID>` 关联 `<datalist id=ID>`，option 的 `value` 优先、缺省用文字。输入即过滤（**大小写不敏感子串**，同 Chrome）、非模态 `Popup` 锚在字段下方（**字段保持焦点**，可继续打字）、`↓/↑` 移动（两端循环）、`Enter` 确认、`Esc` 关闭、点击确认；列表关着时 `↓` 打开。导航键在 **capture 阶段**截获（El 是 backing 字段的祖先）——否则「确认建议」的 Enter 会被字段当成提交表单。`<datalist>` 自身不渲染（UA display:none），因此建议表是从**原始 DOM** 解析的（编译期剪枝会把它从 El 树里拿掉）。程序式入口 `El.SetSuggestions` / `h` DSL `.Suggest(...)`。**已知差异**：浏览器把列表拉成字段宽，这里按最宽建议自适应（`Popup` 尺寸取自 `Content.Measure`，而 Box/Button 自己实现 Measure，`Style().Width`/`SetPreferredSize` 都到不了） |
| `output` | ❌ | 未实现 |

### 替换元素（自绘挂载点）
| 标签 | 状态 | 备注 |
|---|---|---|
| `canvas` | ✅ | 「widget 自绘」挂载点（不给 JS 2D context，直接用 qui 的绘制能力实现画布内容）。`El.SetCanvasDraw(func(cv qui.Canvas, bounds qui.Rect))` 装一个每帧调用的纯绘制回调（内部包成 `canvasLeaf`；动画调 `Invalidate()`）；`El.SetCanvas(qui.Widget)` 插入任意自定义 widget（需事件/动画：自定义 `BaseWidget`、`scene3d.Viewport`、`graphs` 图表…）。CSS `width/height` 定尺寸（缺省 HTML 的 300×150），盒装饰画在其后；未接线时渲染虚线占位框。默认 `display:inline-block`、`Role()=image`。演示见 `examples/html-css` 模块 12（`cv-bars` 柱状图、`cv-art` Lissajous 曲线、`cv-pad` 交互涂鸦板、未接线占位） |

### 明确不支持（🚫 走原生 widgets / 其他包）
| 标签 | 替代方案 |
|---|---|
| `audio` | `media.AudioPlayer` |
| `video` | `media.VideoView` |
| `iframe` / 内嵌浏览器 | `webview`（CEF，需 build tag） |
| `svg`（内联标签） | `<img src=*.svg>` 或 `h.Icon` + `svg.Document`（`qui.VectorSource`） |
| `dialog` | reactive `h.ModalPortal` / `widgets.Dialog` |
| `details/summary` `progress` `meter` | 原生 `widgets`（ScrollView/Progress 等）组合 |
| `text-shadow` | ❌ |
---

## 2. CSS 属性

### 文本 / 字体（可继承）
| 属性 | 状态 | 备注 |
|---|---|---|
| `color` | ✅ | 也推到表单控件文字色（Input/TextArea/Select 的 `Foreground`），仅显式声明时覆盖 UA 默认 |
| `accent-color` | ✅ | 继承；驱动控件 active chrome：checkbox 勾选框 / radio 圆点 / switch 轨道 / slider active track+handle+state layer（`applyControlColors`）。`auto` 回退原生浅色默认。业务暗色主题（qui 只出浅色 theme，暗色走 CSS）的关键属性 |
| `font-size` | ✅ | px/em/rem/pt/% |
| `font-weight` | ✅ | bold/normal/lighter + 数值 |
| `font-style` | ✅ | italic |
| `font-family` | ✅ | 取第一个 family |
| `font`（简写） | ✅ | `expandFont` |
| `line-height` | ✅ | 无单位倍数 / 长度 |
| `text-align` | ✅ | start/center/end/**justify**。justify 只拉伸软换行行（段末/`\n` 前/截断行保持自然），加宽经 root `TextLayoutLine.JustifyExtra` + `Font.WordSpacing` 通道贯穿 plain（Label）与 inline（InlineBox 混排）两条路径——绘制/选择/命中一致（Label 整行选中的高亮 clamp 到排版行宽，避免把被 trim 的换行空格及其 justify 份额算进选区）；富文本 spans 路径（`RichText` widget）不参与 |
| `text-decoration` / `-line` | ✅ | underline / line-through / **overline** + **`-color`**（下划线独立色）+ **`-style`**（solid/double/dotted/dashed/wavy）+ **`-thickness`**（px）；shorthand 内混排 line/style/color/thickness 一并解析（`parseDecorationExtras`，longhand 覆盖 shorthand）。root `DecorationPaint` 贯穿 plain（`ParagraphStyle`）/rich（`TextSpan`）/inline（`InlineItem`）三条绘制路径。限制：wavy 用 zig-zag path 近似 |
| `text-transform` | ✅ | upper/lower/capitalize |
| `overflow-wrap` / `word-wrap` / `word-break` | ✅ | `break-word`/`anywhere`/`break-all` 打开长词内断行（贴 URL、无空格的数字/CJK 长串），`normal`/`keep-all` 溢出（CSS 默认）；继承。两条文本装配同时生效：plain（`ParagraphStyle.BreakLongWords`→`TextLayoutOptions`→`wrapTextLine`）与 inline（`InlineBox.BreakLongWords`→`InlineLayoutOptions`→`splitInlineTok`，切点落在 grapheme 边界、源区间精确故 caret 可穿过断点）。限制：`break-all` 按 `break-word` 近似（能放进下一行的短词仍整体下移，不逐字切）；`hyphens` 未实现 |
| `text-overflow: ellipsis` | 🟡 | 仅配合 `white-space:nowrap` 单行 |
| `white-space` | ✅ | normal / nowrap / `pre`（保留空白+换行、不换行）/ `pre-wrap`（保留+换行）/ `pre-line`（合空格、留换行）。限制：`<pre>` 内混排行内子元素的空白仍走折叠 |
| `letter-spacing` `word-spacing` | ✅ | 经 `qui.Font.LetterSpacing/WordSpacing`：`runeAdvanceWithFaces`（度量 chokepoint，全测量路径共享）+ CPU 逐字形绘制 `drawSpacedRunRaw`（GPU 后端文本委托 CPU，故单路径覆盖两后端）；`normal` 归零；均继承；tracking 随 canvas scale 缩放（`imageCanvas.DrawText`——曾在 2× 屏上画出半强度，justify 上线时暴露并修复）。限制：spacing 路径下合成斜体 shear 退化 |
| `text-indent` | ✅ | 首行缩进：`ParagraphStyle.FirstIndent`→`TextLayoutOptions.FirstIndent`，首行 wrap 宽度减 indent + 首行 Offset 右移；继承。限制：与 `text-align:center/end` 且**无界宽度**组合时缩进被对齐重算覆盖（有界/左对齐正常） |


### 盒模型 / 边框
| 属性 | 状态 | 备注 |
|---|---|---|
| `width` `height` `min-*` `max-*` | 🟡 | px/em/rem/pt/calc()；常规流 block 现认显式 width/height（左对齐、不撑满；min/max clamp）+ flex/grid/inline-block。**`%` 参照包含块**：布局期解析 width/height（`qui.Style.WidthPct/HeightPct`→FlowLayout band / Flex basis+cross）**及 min/max**（`Min/MaxWidthPct` 等→`resolvedMinSize/resolvedMaxSize`，FlowLayout block + flex item；如 `max-width:100%` 防溢出），解析结果按 border-box 计（忽略 box-sizing）。**欠**：`calc()` 内含 `%`、inline-block/绝对定位的 `%` |
| `padding` `margin`（简写 + 单边） | ✅ | 1–4 值规则；margin 有 CSS margin-box 语义（Flex/Flow）；**`margin: 0 auto` 水平居中**（FlowLayout：双 auto 居中、仅 left auto 靠右）；**flex 项 auto margin**（主轴吸收正剩余空间并压制 justify-content、交叉轴 auto 居中覆盖 align；四边 `qui.Style.Margin{Left/Right/Top/Bottom}Auto`→FlexLayout）。**欠**：flex 项 auto margin 在 overflow(负空间)下按 0（合规） |
| `border` / `border-width/color/style` | ✅ | `border: none` 显式声明会**清除**控件的原生边框（`HasBorder` 区分「声明了 none」与「未声明」，applyCommon） |
| `border-<side>*` 单边 | ✅ | |
| `border-style` | 🟡 | solid/dashed/dotted/none；double/groove/ridge/inset/outset → 退化 solid |
| `border-radius` | ✅ | 统一半径 + **单角**（`border-top-left-radius` 等）+ **1–4 值简写**（顺时针 TL/TR/BR/BL，全等塌回统一快路径）+ 椭圆 `a / b`（取水平半径）。root `Path.AddRRectCorners`（含 CSS 相邻角溢出按边缩放）→ `Style.Corners`；`box.go` 经 `ShapePath` 填充/描边、`overflow:hidden` 用 `ClipPath` 圆角裁子元素。限制：box-shadow 仍按统一 `Radius`（per-corner 阴影退化） |
| `box-sizing` | ✅ | 默认 **content-box**（规范：width/height=内容盒，padding+border 加在外）；`border-box`=声明值即外框。在 `applyCommon` 换算成 qui 外框宽高。注：qui border 画在内、不额外占布局，故 content 面积可能差 border 厚度（可忽略） |
| `outline` | ✅ | `outline`/`outline-width`/`outline-color` → 边框外描一圈（`El.Draw` stroke，不占布局）；无 `outline-offset`/style | |

### 背景 / 视觉效果
| 属性 | 状态 | 备注 |
|---|---|---|
| `background` / `background-color` | ✅ | 简写按颜色解析 |
| `background-image: linear-gradient()` | ✅ | 角度 + `to <side>`；多色标 |
| `background-image: url()` | 🟡 | 位图背景经 `qui.ImageShader` 拉伸铺满（含 `data:` URI）；**无** `background-size/position/repeat`（恒 100% 拉伸） |
| `radial-gradient` | ✅ | 忽略 shape/size/position 前缀，恒 farthest-corner 径向；多色标 |
| `conic-gradient` | ✅ | `from <angle>` + 多色标（`qui.ConicGradient`）；position 忽略（居中） |
| `repeating-*` gradient | ❌ | |
| `background-size` | 🟡 | `cover`/`contain`/拉伸（`qui.ImageFit`）；**无** 显式尺寸 / `position` / `repeat` |
| `box-shadow` | 🟡 | 多层（逗号分隔，`Style.ExtraShadows`）；**inset 忽略**（无内阴影 primitive） |
| `opacity` | ✅ | |
| `transform` | ✅ | translate/rotate/scale/**skew**/**matrix()**（2D，经 `Canvas.Concat`）+ **`transform-origin`**（关键字/%）；**无** 3D |
| `filter` | 🟡 | `blur()`（`qui.BlurImageFilter` 三次盒糊）+ `drop-shadow()`（`DropShadowImageFilter`）经 SaveLayer；多函数取首个；无 brightness/contrast/… |
| `backdrop-filter` | ❌ | 需背景采样 |

### 布局 / 定位
| 属性 | 状态 | 备注 |
|---|---|---|
| `display: block/inline/inline-block/none/flex/grid` | ✅ | 静态 `none` 编译期剪除；运行时切换 `none` 也生效且**父布局彻底跳过该盒**（无 flex/grid gap 残留；root `Collapsed` 语义，`Container.inFlowChildren`/Draw 过滤） |
| Flex：`flex-direction/justify-content/align-items/flex-wrap/gap/flex-grow` | ✅ | `align-items:stretch`（默认）只拉伸**交叉轴为 auto** 的项：显式 `width`（column 容器）/`height`（row 容器）——含百分比——保持自己的尺寸并贴行首，同浏览器（root `flexItem.explicitCross`→`resolveCross`）；min/max 仍 clamp |
| Flex：`align-self/flex-shrink/flex-basis/order` | ✅ | `flex` 简写展开 grow/shrink/basis（none/auto/initial 关键字）；`order` 走 root `FlexItem.Order`（稳定排序）；`flex-shrink:0`→NoShrink |
| Flex：`align-content` | ✅ | wrap 多行在剩余交叉轴空间的分布：start/center/end/space-between/around/evenly/stretch（root `FlexLayout.AlignContent`）；未设置按 CSS 初始值 stretch |
| Grid：`grid-template-columns/rows`（px/fr/auto/repeat）、`gap`/`row-gap`/`column-gap` | ✅ | |
| Grid：单元格放置 `grid-column/row`、`span`、`grid-template-areas` | ✅ | 行/列线号(1-based `a / b`)、`span N`、`grid-area`(命名区或 4 线号)；`grid-template-areas` 命名区 + 隐含轨道数(单/双引号)。子项自身 `applyComputed` 里解析(父 grid 先算)。**限制**：仅 `grid-column/row` 简写(不读 `*-start/*-end` 拆写)；混合"显式一轴+自动另一轴"退化为纯 span 自动排布 |
| `position: relative` | ✅ | top/right/bottom/left 偏移；建立包含块 |
| `position: absolute` | ✅ | 脱离常规流，相对最近 positioned 祖先的盒定位（root 层：`BaseWidget.OutOfFlow`+`Container.layoutAbsoluteDescendants`）。限制：包含块用 border box 近似（非 padding box，差 border 厚度）；无 `z-index` 层叠；`auto` 侧（未给 top/left…）落在包含块原点而非静态位。可作常规流 block 的角标（block width 已生效） |
| `position: fixed` | 🟡 | 当作 absolute（相对最近 positioned 祖先/root），**不随视口固定** |
| `position: sticky` | ❌ | |
| `overflow: hidden/clip/auto/scroll`（含 -x/-y） | ✅ | auto/scroll 托管 `ScrollView` |
| `visibility: hidden/collapse` | ✅ | 保留布局盒、不绘制（自身+子树）；限制：子孙 `visibility:visible` 反显未支持 |
| `z-index` | 🟡 | 绘制顺序按 z 升序（`qui.ChildrenInPaintOrder`，paint 与布局解耦）；**无完整 stacking context**（不建新层叠上下文、不隔离子树） |
| `float` `clear` `columns` | ❌ | 无浮动 / 多列 |

### 交互 / 指针

| 属性 | 状态 | 备注 |
|---|---|---|
| `cursor` | ✅ | 继承。root 层**声明式**解析（`qui/cursor.go`）：`hoverPath` 每次 mouse move 求值一遍，**两级优先**——① 声明值（CSS / `BaseWidget.SetCursorShape`）由内向外取第一个；② 都没声明才问 widget 内建形状（`CursorProvider`/`CursorAtProvider`：Label/InlineBox 的链接手型 + 文本 I 形、Input/TextArea 的 I 形、Anchor 手型、tooltip 气泡）。这条规则就是 `cursor:pointer` 的卡片能压住内部文字 I 形的原因，同时无声明的页面保留各 widget 原生手感。UA 表给 `a[href]` 加 `pointer`（无 href 的 `<a>` 不算链接，不给）；`<button>` 按浏览器行为**不**给 pointer。映射：pointer→Hand、text/vertical-text→Text、crosshair/cell→Crosshair、ew/col/e/w-resize→ResizeEW、ns/row/n/s-resize→ResizeNS。**限制**：GLFW 3.3 只有 6 种标准光标，`not-allowed`/`move`/`grab`/`wait`/`help`/`zoom-*`/斜向 resize **算「已声明」但退化成箭头**（声明仍生效——能压住嵌套 I 形，这是意图里能兑现的部分）；要真形状需在平台缝里加 `CursorShape` + cocoa/glfw 映射（未立项）。不支持 `url()` 自定义光标（跳过、取列表里第一个关键字） |
| `user-select` | ✅ | 继承，含 `-webkit-user-select` 别名。`none` 把元素文字移出拖选 + 剪贴板（`Label/InlineBox.Selectable=false`）；`text`/`auto`/`all`/`contain` 复原（可覆盖继承来的 `none`）。控件面（`<label for>`、push-button input）即使没声明也不可选。**双向赋值**：restyle 掉规则会把可选性交还（El 复用，旧代码只会关不会开） |
| `pointer-events: none / auto` | ✅ | 继承。root 层在**选目标处**过滤（`qui/pointer_events.go` + `Container.HitTest`/`Window.hitTestAll`），所以 hover / focus / tooltip / cursor / drag 看到的是同一个目标：透明元素自己不当目标，但**先下钻子元素**——于是 `pointer-events:auto` 的后代能重新开洞（浏览器行为）。透明层照常布局/绘制/动画。SVG 专用值（`visiblePainted`/`fill`/…）一律按「可命中」 |

### 选择器 / 变量 / at-rules
| 特性 | 状态 | 备注 |
|---|---|---|
| 标签 / `.class` / `#id` / `*` | ✅ | |
| 属性选择器 `[a]` `=` `^=` `$=` `*=` `~=` `\|=` | ✅ | 大小写不敏感标志 `i` 支持（`s` 即默认行为、被吸收） |
| 组合器（后代 / `>` / `+` / `~`） | ✅ | 右到左匹配 |
| `:hover` `:focus` `:active` | ✅ | `focus-within/visible` 归一为 focus |
| **祖先/兄弟状态触发后代**（`.row:hover .del`、`.a:hover ~ .b`、`.row:focus(-within) .del`、`.row:active .del`） | ✅ | 非主体 compound 的 `:hover`/`:focus`/`:active` 由**选择器点名的那个元素**的状态驱动，切**盒装饰/文字色/`visibility`**。触发器精确发现（`Stylesheet.stateTriggersFor` 逐候选匹配：祖先 + 前置兄弟/祖先的前置兄弟，后者仅当表里有兄弟组合器状态规则）；payload **按当前活跃触发器组合**算 variant（`El.ancestorStateVariant` 按 mask 惰性级联缓存）——hover `.outer` 不会连带应用点名 `.row:hover` 的规则。重绘：hover/press 边界走触发器 `El.Handle`→精确 invalidate 依赖元素（兄弟揭示在触发器 rect 外也能重绘）；focus 走 `Window.AddFocusChangeListener`（engine 订阅）。`:focus` 判定为 **focus-within**（焦点在触发器子树内即可，含 `<input>`）；主体状态向祖先 compound 传导（悬停/按压元素 ⇒ 祖先也 hover/active、focus ⇒ 祖先 focus-within），所以 **`.a:hover .b:hover` 已覆盖**（落主体 Hover variant）。**限制**：`display:none`→揭示未支持（涉布局，仍走静态剪枝）；一条规则带**两个**非主体状态 compound（`.a:hover .b:hover .c`）不发现触发器；`.row:focus .del` 中 row 自身聚焦需 row 可聚焦（有自身 `:focus` 盒规则或内含控件） |
| `:root` `:first-child` `:last-child` `:only-child` `:nth-child(An+B)` `:not(simple)` | ✅ | |
| `:nth-of-type(An+B)` | ✅ | 按同 tag 计数（`elementIndexOfType`） |
| `:checked` `:disabled` | ✅ | 属性驱动 + **运行时实时联动**：用户勾选/切换单选、或 `El.SetDisabled(bool)`，会同步 `checked`/`disabled` 属性并触发作用域 restyle，使 `:checked`/`:disabled`（含 `:has(:checked)` 等祖先/兄弟规则）即时重级联。checkbox `OnChange`/radio group `OnChange`/`SetChecked` 走引擎包裹器统一 relink |
| `:enabled` `:required` `:optional` `:read-only` `:read-write` | ✅ | 属性驱动（同 `:checked`/`:disabled` 的路子）。`:enabled`/`:required`/`:optional` 只作用于表单控件（input/select/textarea/button/fieldset/optgroup/option）；`:read-only` 按规范匹配**一切非用户可改**的元素（`<p>` 也算），`:read-write` 只匹配可编辑控件（文本类 input/textarea 且无 readonly/disabled，或带 `contenteditable`）。**欠**：`:valid`/`:invalid`/`:placeholder-shown`/`:indeterminate`/`:default`（需「值变化 → restyle」的实时钩子，见 §4 Tier A） |
| `:nth-last-child(An+B)` `:empty` `:has()` | ✅ | `:has()` 限单个简单后代选择器（`div:has(img)`）；未知伪类仍 fail-closed |
| `::before` `::after` | 🟡 | 生成 `content` 文本(叶/行内元素折进同一 InlineBox `[before][text][after]`，继承 color/font)。**`content` 支持**：引号字符串 + **`attr(name)`**(取元素属性值，缺则空；`attr(x, def)` 取名忽略类型/兜底) + 二者拼接(`"[" attr(x) "]"`)。**限制**：带块级子元素的元素上不生成；`<li>`/折叠进父 run 的行内元素上不生成(用叶/块级叶元素承载)；不支持 `counter()`/生成盒的完整盒模型 |
| 其他伪元素(`::first-line`…) | ❌ | 解析但不生成 |
| CSS 变量 `--x` / `var(--x, fallback)` / `:root` | ✅ | 继承 + 递归解析（深度上限 16） |
| 简写 `font` `flex` `inset` | ✅ | |
| `@media` | 🟡 | `min-width`/`max-width`（`and`、screen/all/print）在**解析时**对视口宽度求值(`Options.ViewportWidth` 或窗口宽，默认 1280)，匹配块扁平进样式表；**非响应式**(不随 resize 重算) |
| `@font-face` `@keyframes` `@import` `@supports` | ❌ | at-rule 整块跳过 |
| `!important` / 级联优先级 / 内联 style | ✅ | UA < author < inline，important 跨层 |
| 运行时换样式表 `StyleEngine.SetCSS` | ✅ | 重解析（沿用构建时 @media 视口）+ 全量 Restyle，树原地重级联——主题切换钩子（换一套 :root 变量即可）；`RenderResult.Engine` 暴露静态渲染的引擎 |
| 颜色：hex 3/6/8、`rgb()`/`rgba()`、`hsl()`/`hsla()`、具名色全集 | ✅ | hsl 支持逗号式与空格式(`h s% l% / a`)、hue 可带 `deg`、负 hue 环绕；`background` 简写整值优先解析(rgb/hsl 带逗号也认)；**CSS Level 4 具名色全集 148 色 + transparent**。无 `hwb()`/`lab()`/`lch()` |
| 长度：px/em/rem/pt/%/无单位/`calc()`/`min()`/`max()`/`clamp()` | 🟡 | `calc()` 支持 `+ - * /`、括号、嵌套、单位混算；`min/max/clamp` 各参数经 `parseLength`(可嵌 calc)。**限制**：内含 `%` 只在该属性本身支持 `%` 参照时有效。**无** `vw/vh/ch`(需视口尺寸接入布局) |
| `transition` / `animation` | ❌ | 动态基建暂缓（见 htmlcss-engine-direction 记忆） |

---

## 3. 事件与交互

| 能力 | 状态 | 备注 |
|---|---|---|
| 点击 `onClick` | ✅ | |
| 右键 `onContextMenu` | ✅ | 回传窗口坐标，可锚定菜单 |
| `<a href>` 导航 | ✅ | 元素本身或折叠 span 均可点 |
| `:hover`/`:active` 盒装饰反馈 | ✅ | button 无 author 规则时有内建 darken/press |
| **`:hover`/`:focus`/`:active` 改文字颜色 + text-decoration** | ✅ | 独立元素 + 段落里折叠的行内链接均支持（有状态色规则的行内元素保留为原子 El，`El.applyStateText` draw 时按态切 color+下划线）。原子行内元素通过 `El.InlineBaseline`（`qui.InlineBaselineProvider`）与周围文字**共享基线**（不再是盒底压基线而显得偏高）。**限制**：状态改 `font-weight/size` 因会重排，未在 draw 时应用（见 P2） |
| `:focus` 盒样式 | ✅ | focusable 盒子会禁用子 Label 选择以抢焦点 |
| 输入 `onInput` / 回车 `onSubmit` | ✅ | input/textarea |
| checkbox `onToggle` / select `onChange` | ✅ | 用户切换会同步 `checked` 属性 + relink `:checked`（见选择器行） |
| `<form>` 提交 `onFormSubmit` | ✅ | `SetOnFormSubmit(map[string]string)`；回车/submit `<button>` 触发，收集具名控件值（见 `<form>` 标签行） |
| **`onMouseEnter` / `onMouseLeave`** | ✅ | 每次进出各触发一次（窗口按命中路径 diff 合成，DOM mouseenter/leave 语义、不冒泡）。给「悬停要做事」用（预取/预览/tooltip 状态）；纯外观仍走 `:hover` 规则（零渲染开销） |
| **`onDoubleClick`** | ✅ | 两次左键释放在 400ms + 5px 内合成（用事件自带 `When` 而非墙钟，测试/录制回放行为一致）。两次单击照常各触发一次 `onClick`，且 **`dblclick` 在两次 `click` 之后派发**（DOM 顺序 click→click→dblclick，实测发现过一次反序并修正）；第三次点击重新起算 |
| **`onWheel`** | ✅ | 收 `(dx, dy)`，**返回 true 才消费**——否则外层 ScrollView 继续滚动（等价于 DOM 里不调 preventDefault）。冒泡阶段执行，内层优先 |
| **`onKeyDown` / `onKeyUp`** | ✅ | 收 `qui.KeyEvent`，返回 true 消费。两条到达路径：元素自己持有焦点（**声明 key handler 即变可聚焦**，`El.Focusable`），或事件从聚焦的**后代**冒泡上来——后者就是对话框接 Esc / Cmd+Enter 而不抢走输入框焦点的做法。disabled 元素吞键不触发（对齐它吞点击的行为） |
| **`onFocus` / `onBlur`** | ✅ | 在焦点变化的**跃变**上各触发一次（`El.SetFocused` 包住 `Box` 的记账）。表单元素的焦点在 backing 控件上，回调跟随该控件 |
| 跨 widget 文本选择 | ✅ | Label + InlineBox 实现 `TextSelectable` |
| 剪贴板复制（纯文本 + HTML flavor） | ✅ | 图片内联 data URI、列表重建 `<ol>/<ul>`、**表格重建 `<table>`（HTML→Google Docs/Word 真表格；plain→TSV 供 Sheets）**。混排选区（富文本段 + 列表 + 表格 + 段落同选）各自成结构、按文档顺序、互不吞并；空单元格补齐保持列对齐。**选区坐标归一到包围盒**（从选中最小行/列起算，不再补前导空行空列）；**单个单元格选区**直接复制其内容、不套 `<table>`/TSV。**限制**：单元格内嵌套列表/多块 flatten 成 `<br>` 分隔（非嵌套 `<ul>`）；整块选区里跨行/跨列合并单元格的对齐为近似 |
| 拖拽重排 Drag & Drop | ✅ | Draggable/DragHandle/OnDrop/OnDragOver，paint-only Transform 反馈 |
| **`app-region: drag / no-drag`**（自绘标题栏拖窗，Electron `-webkit-app-region` 同名别名） | ✅ | 可继承：`drag` 声明在标题栏条上，整个子树都能拖窗；`no-drag` 子元素（tab/按钮）挖回来。落到 `qui.Window.BeginWindowDrag()`（macOS `performWindowDragWithEvent:`，自带吸附 + 双击 zoom）。**只在 `Window.SetTitlebarStyle(qui.TitlebarOverlay)` 的窗口有意义**；不支持的平台原样返回 false、行为退化成普通按下。示例 `examples/chrome-tabs` |
| 键盘焦点 / Tab 循环 | ✅ | 框架层；控件编辑走 backing widget |
| HTML `disabled` 属性 | ✅ | 禁用 backing 控件（`SetEnabled(false)`）；`El.SetDisabled(bool)` 运行时切换并 relink `:disabled` |
| HTML `title` 属性 → hover tooltip | ✅ | applyComputed 时推到 widget `SetTooltip`（窗口级 TooltipProvider）；折叠进文本 run 的行内元素无独立 widget、不生效（用原子元素承载）；`h.Builder.Title()` |
| HTML `hidden` 属性 | ✅ | 按 UA 规则实现（`[hidden] { display: none }` 在 tier 0 发一条 `display:none`），因此**作者 CSS 可以覆盖**它（同浏览器）。静态 `Render` 下命中 display:none 的编译期剪枝（元素不产生 widget） |
| HTML `tabindex` / `accesskey` / `minlength` / `pattern` / `rows` / `cols` / `autofocus` / `inputmode` / `spellcheck` 属性 | ❌ | 不读取 |
| `label[for]` 点击聚焦控件 | ✅ | 见标签行；toggle/select/focus |
| AX 角色（button/link/textbox/img/list/heading…） | ✅ | `El.Role()` + `AccessibleName()`，agent 可寻址 |

---

## 4. 缺口清单（Gap List）——「原生 HTML/CSS 有、qui 没有」

> **为什么单列一节**：上面 §1–§3 的矩阵只列「已经想到的特性」，
> 没被列进去的特性等于不可见——于是读文档时的覆盖率感知会明显高于实际。
> 本节是**反向清单**：以浏览器的原生能力为基准，列出 qui 尚不支持的项，
> 按「原生 × 桌面应用高频」分三档。**Tier C 是明确不做**（写在这里就是为了
> 让「不做」也成为一个有记录的决定，避免反复评估）。
>
> 评估基线：2026-08-13，逐项对着 `compute.go` 的属性读取、`css.go` 的
> `pseudoKind`、`el.go` 的事件 setter 与 `attrs[...]` 读取点核过。
> 新增支持时：把行从这里删掉 → 在 §1/§2/§3 矩阵加行 → 在 §6 登记测试。

### Tier A — 原生 + 日常必用 + 目前零支持（优先补）

| 缺口 | 现状 | 备注 / 落点 |
|---|---|---|
| ~~`cursor`~~ | ✅ 2026-08-13 | 见 §2「交互 / 指针」。root 层两级声明式解析（`qui/cursor.go`）；残留：6 种以外的形状退化成箭头 |
| ~~`pointer-events: none`~~ | ✅ 2026-08-13 | 见 §2「交互 / 指针」。`qui/pointer_events.go`，命中测试处过滤 |
| ~~`user-select: none`~~ | ✅ 2026-08-13 | 见 §2「交互 / 指针」 |
| `aspect-ratio` | ❌ | 卡片/缩略图/视频占位；需 Measure 期按一轴推另一轴 |
| `position: sticky` | ❌ | 表头吸顶、侧栏；需与 `ScrollView` 滚动偏移联动（见 §5「仍待办」） |
| `::placeholder` | ❌ | 伪元素名已被解析但不生成；placeholder 文字色/字体是表单样式的标准写法 |
| `:valid` `:invalid` `:placeholder-shown` `:indeterminate` `:default` | ❌ | ~~`:required`/`:optional`/`:read-only`/`:read-write`/`:enabled`~~ ✅ 2026-08-13（属性驱动，见 §2）。剩下这几个都要**「值变化 → 作用域 restyle」的实时钩子**：`:placeholder-shown` 看当前值是否为空、`:valid/:invalid` 还要一个校验模型（required 空值 + type=email/url + `pattern`）。建议和 `::placeholder` 同批做 |
| `::selection` `::marker` | ❌ | 选区配色、列表标记独立着色 |
| `:is()` / `:where()` / CSS 嵌套 `&` | ❌ | 纯选择器解析层，独立性强，可随时插队 |
| ~~键盘 / 焦点 / 鼠标事件~~ | ✅ 2026-08-13 | 新增 `onKeyDown/onKeyUp`、`onFocus/onBlur`、`onMouseEnter/onMouseLeave`、`onDoubleClick`、`onWheel`（见 §3；`h` DSL 同名链式方法）。**仍缺**：`mousemove`（高频，需想清楚重绘代价）、`scroll`（滚动位置变化，需 ScrollView 事件）、`change`（与 `onCommit` 语义重叠，待定是否单列） |
| `tabindex` `minlength` `pattern` `rows` `cols` `autofocus` `inputmode` `spellcheck` 属性 | ❌ 不读取 | ~~`hidden`/`readonly`/`maxlength`/`required`~~ ✅ 2026-08-13（见 §1/§3；`required` 目前只驱动 `:required`/`:optional` 选择器，尚无提交时校验） |
| `transition` | ❌ | 交互手感；与「动态基建」一起立项（见 §5） |

### Tier B — 原生 + 常用 + 值得排期

| 缺口 | 现状 | 备注 |
|---|---|---|
| Grid `justify-items` / `justify-self` / `place-items` / `place-content` / `align-content` | ❌ | 当前 Grid 只有 template/gap/显式放置；单元格内对齐无 |
| Grid `grid-auto-flow` / `grid-auto-rows` / `grid-auto-columns` / 隐式轨道 | ❌ | 自动排布只有默认行为 |
| `min-content` / `max-content` / `fit-content` 尺寸关键字 | ❌ | 表格与自适应卡片常用 |
| `@media` 响应式重算 | 🟡 解析期一次性 | resize 不重级联；与 `transition` 共用动态基建 |
| `@media (prefers-color-scheme)` / `(hover)` / `(pointer)` / `(prefers-reduced-motion)` | ❌ | 暗色主题的标准入口 |
| `@container` 容器查询 | ❌ | |
| `color-scheme` | ❌ | |
| `oklch()` / `color-mix()` / `light-dark()` / `hwb()` / `lab()` | ❌ | 主题系统会很想要（需要自己实现色彩空间换算） |
| `text-shadow` | ❌ | 见 §1 末尾与 §5 |
| `text-wrap: balance/pretty` · `hyphens` · `tab-size` · `font-variant*` · `font-feature-settings` · `text-underline-offset` | ❌ | 文本抛光 |
| `background-position` / `-repeat` / 显式 `background-size` / `repeating-*-gradient` | ❌ | 现恒 100% 拉伸（见 §2） |
| 远程图片 URL / `srcset` | ❌ | 需网络加载 + 缓存 + 异步重排 |
| `resize`（textarea）· `appearance: none` · `caret-color` · `scrollbar-width/color` · `overscroll-behavior` · `scroll-behavior` · scroll-snap | ❌ | 控件/滚动抛光 |
| `<details>/<summary>` · `<progress>` · `<meter>` · `<dialog>` | 🚫→建议改 ❌ | §1 现标「走原生 widgets」，但底层 widget 都齐（Progress/Dialog/ScrollView），映射成本低，而 accordion/进度条在 HTML 里写更自然。**这条边界建议重划** |
| `direction: rtl` / bidi | ❌ | 仅在做国际化时算高频 |
| `<datalist>` / `<output>` | ❌ | datalist 是原生 combobox（输入过滤补全）的正统做法 |
| `<select multiple>` / `size` / `<option value>` / `<option selected>` / `<option disabled>` / optgroup 分组标题 | ❌ | **详见下方「select 专项」** |
| `<input type=search>` 的 UA 清空按钮 · number 步进器 · 原生 date/color 选择器 | ❌ | 前两项纯前端可做；后者需 NSDatePicker/NSColorPanel cgo 桥（未立项） |
| `<a target>` / `download` | ❌ | |

### Tier C — 原生但桌面应用低频 / 代价过高：**明确不做**

`float` / `clear` · `columns`（多列）· `@keyframes` + `animation`（`transition` 之外的关键帧）·
`writing-mode` · `clip-path` / `mask` · `mix-blend-mode` / `isolation` · 完整 stacking context ·
`backdrop-filter`（需背景采样）· 3D `transform` · `contenteditable` · `<iframe>`（走 `webview`）·
Shadow DOM / `<template>` / `<slot>` · `content-visibility` / `will-change` · `@import` · `@supports`。

### select 专项（2026-08-13 已大部分落地）

已修（都在 §1 `select` / `input[type=color]` 行里）：

- ~~**`<option value>` 被完全忽略**~~ ✅ —— 原症状：`static.go` 只收集 option 的可见文字，
  `controlNameValue` 返回 `sel.SelectedValue()`，于是
  `<option value="us">United States</option>` 提交上来是 `United States`。
  现在 El 维护 `selectItems`（显示）/`selectValues`（提交）两条平行数组。
- ~~**`<option selected>` 不读**~~ ✅ —— 设初选，且记进 `defaultSelectedIdx` 供 `<form>` reset 还原。
- ~~`<option disabled>` / optgroup 分组标题~~ ✅ —— 新增 `widgets.Select.ItemDisabled`
  （`MenuItem.Disabled` 早就有，成本很低），点击/方向键/type-ahead 三条路径都跳过。
- ~~**原生 select 的键盘 typeahead**~~ ✅ —— `widgets.Select.typeAhead`。
- ~~`input type=color` 提交初始值而非用户选的颜色~~ ✅ —— 顺带修，reset 也补上了。
- ~~`<input type=search>` 的 UA 清空按钮~~ ✅ —— 新的 `Input.TrailingButton` 内嵌附件机制。

- ~~`<datalist>`（原生补全）~~ ✅ 2026-08-13 —— 见 §1 `datalist` 行。这才是「补全」的原生正解。
- ~~number 步进器~~ ✅ 2026-08-13 —— 复用了 `Input.TrailingButton` 机制。
- 顺带修的真 bug：`El.SetOnInput` 过去把作者回调**直接**绑到 backing widget 的 `OnChange` 上，
  覆盖了引擎自己的包装器——于是一旦业务接了 `onInput`，datalist 补全就静默失效。
  现在统一走 `El.bindInputChange`（作者回调先跑，引擎的补全刷新后跑）。

仍缺：`multiple`、`size`、真嵌套分组（现为标题行+平铺近似）、建议列表宽度不跟字段。

- **边界声明（不变）**：「清空按钮（allowClear）」「多选 + 搜索 + tag 化」**不是原生 HTML**，
  是 Ant Design / Element Plus 那层的组件语义。原生对应物是
  `<input list=datalist>`（补全）、`<input type=search>` 的 UA 清空按钮、
  以及一个空 `<option value="">`。qui 的分工：**原生语义补在 `htmlcss`，
  组件语义（allowClear/tag/远程搜索）做成 `reactive/html` 组件**，不塞进引擎。

### 建议排期（本节的执行顺序）

1. **P0「交互手感原生化」**：~~`cursor`~~ ✅、~~`pointer-events`~~ ✅、~~`user-select`~~ ✅、
   ~~`readonly`/`maxlength`/`hidden` 属性~~ ✅、~~`:required`/`:optional`/`:read-only`/`:read-write`/`:enabled`~~ ✅、
   ~~键盘/焦点/鼠标事件~~ ✅。**仍剩**：`::placeholder` + `:placeholder-shown`/`:valid`/`:invalid`（共用同一个「值变化 → 作用域 restyle」实时钩子）。
2. **P1「表单语义闭环」**：~~`<option value/selected/disabled>` + optgroup 标题~~ ✅、
   ~~color 序列化 bug~~ ✅、~~`type=search` 清空~~ ✅、~~select typeahead~~ ✅。
   ~~`datalist`~~ ✅、~~number 步进器~~ ✅（都在 §1 对应行）。
   **仍剩**：`multiple` / `size`（需多选交互模型，`widgets.Select` 目前是单选语义）。
3. **P2「布局补齐」**：`position:sticky`、`aspect-ratio`、Grid `justify-items`/`place-*`/`auto-flow`、
   `min/max-content`。
4. **P3「动态基建」一次立项**：`transition` + `@media` 响应式重算 + `prefers-color-scheme`
   三者共用「时间/环境驱动 restyle」的同一套基建，分开做会重复三遍。
5. **随时可插队**：`:is()`/`:where()`/CSS 嵌套（纯解析层）。

---

## 5. 优先级队列（待办路线图）

> 判据：应用开发高频程度 × 当前「静默降级/易踩坑」程度。改完把对应矩阵行更新，并从此处删除。

### P0 — ✅ 已完成（2026-07-08）
- [x] **`:hover`/`:focus`/`:active` 文字颜色 + text-decoration**：`El.applyStateText` draw 时按状态切
      label/marker/icon 的 color+下划线；`Box.Focused()/Pressed()` 新增。
      残留（降级为 P2）：折叠行内链接重着色、状态改字重/字号。
- [x] **`<input type=radio>` + radio group**：`widgets.RadioButton`，引擎按 `name` 持有 `RadioGroup` 单选。
- [x] **运行时 `display:none` 切换**：`El` 计算出 `display:none` 时零尺寸/不绘制/清空子节点（`El.Measure/Draw`）。
      2026-07-09 补完：父布局彻底跳过（root `Collapsed`），gap 无残留。

### P1 — 高频排版能力缺口
- [x] ~~`<pre>`/`white-space: pre(-wrap)` 保留空白与换行~~ ✅ 2026-07-08。
- [x] ~~`position: absolute`~~ ✅ 2026-07-08：root 层脱流机制（`BaseWidget.OutOfFlow`/`EstablishesAbsContainingBlock`，`Container` 过滤+定位）。
- [x] ~~`box-sizing`~~ ✅ 2026-07-08：走规范，默认 content-box，支持 border-box。
- [x] ~~FlowLayout block 子元素显式 width/height~~ ✅ 2026-07-08：`resolveFlowBlockSize`——block 认显式 width（左对齐、不撑满）+ height + min/max，auto 仍填充。全仓回归通过。
- [x] ~~表格布局 `table/tr/td/th`~~ ✅ 2026-07-08（Phase 1）：拍平成 `GridLayout`（`table.go`），列对齐 + auto 列宽 + colspan + 行条纹 + shrink-to-fit（root 加 `ShrinkToFitWidth` 接口）。Phase 2（下方 P2）：border-collapse / rowspan 行高 / caption / colgroup 宽 / table-layout:fixed。

### P2 — ✅ 大批完成（2026-07-08）
- [x] ~~折叠行内链接 `:hover` 重着色~~；原子行内元素基线对齐（`El.InlineBaseline`）。
- [x] ~~`hsl()/hsla()` + `calc()` + `min()/max()/clamp()`~~（`values.go`）。欠：`vw/vh/ch`。
- [x] ~~`%` 宽高参照包含块 + `margin: 0 auto` 水平居中~~ ✅ 2026-07-09（root `Style.WidthPct/HeightPct` + `MarginLeftAuto/RightAuto`，FlowLayout/FlexLayout 布局期解析）。
- [x] ~~Flex `align-content`~~ ✅ 2026-07-09（root `FlexLayout.AlignContent` 七种模式；htmlcss 未设置按 CSS 初始值 stretch）。
- [x] ~~父级跳过 `display:none` 子项~~ ✅ 2026-07-09（root `Collapsed`；gap 无残留）。
- [x] ~~CSS 具名色全集 + 属性选择器 `i` 标志~~ ✅ 2026-07-09（148 色；`[a="v" i]` 大小写不敏感匹配）。
- [x] ~~选择器 `:nth-of-type`/`:checked`/`:disabled`/`:nth-last-child`/`:empty`/`:has`~~（`css.go`+`dom.go`）。
- [x] ~~Flex `align-self`/`flex-shrink`/`flex-basis`/`order`~~（root `FlexItem.Order`）。欠：`align-content`。
- [x] ~~Grid 显式放置 `grid-column/row/span` + `grid-template-areas`~~（`grid.go`）。
- [x] ~~`input` type：password 掩码 / range 滑块~~；number/email/…→文本框；`label[for]`、`disabled` 属性、`fieldset/legend`。
- [x] ~~`background-image: url()`（+`data:`）+ `radial-gradient` + `conic-gradient` + `background-size:cover/contain`~~（`qui.ImageShader/ConicGradient`）。
- [x] ~~`visibility:hidden` / `outline` / `dl·dt·dd·figure/fieldset/legend` UA / `disabled` 属性 / `object-fit`~~。
- [x] ~~表格 Phase 2 全部（caption / colgroup / table-layout:fixed / border-collapse / rowspan 行高）~~。
- [x] ~~`transform` skew/matrix/`transform-origin`；`box-shadow` 多层；`z-index`（paint 序解耦）；`@media`(min/max-width 静态)；`::before/::after`(叶/行内文本)；`text-decoration:overline`；`filter: blur()/drop-shadow()`；`select optgroup`~~。

### 仍待办（需专门的引擎子系统，逐项独立立项）
- [ ] **`transition`/`animation`**：需动态基建（时间轴驱动 restyle，配合 `anim` 包）。
- [ ] **`@media` 响应式重排**：现为解析期一次性求值；resize 重级联需动态基建。
- [x] ~~`::before`/`::after` `attr()`~~ ✅ 2026-07-08：`content` 支持 `attr(name)` + 引号字符串拼接（`unquoteContent(v, n)`）。仍待：带块级子元素/`counter()`/生成盒盒模型（现仅叶/行内文本）。
- [ ] **`position:fixed`(真视口) / `sticky`**：需与 ScrollView 滚动偏移联动。
- [ ] **`z-index` 完整 stacking context**：现为同级 paint 序，无层叠上下文隔离。
- [x] ~~`letter-spacing`/`word-spacing`/`text-indent`~~ ✅（`qui.Font` tracking + `runeAdvanceWithFaces` 度量 chokepoint + `drawSpacedRunRaw` CPU 逐字形绘制；GPU 文本委托 CPU 故单路径即覆盖两后端）。
- [x] ~~`text-decoration-color`/`-style`/`-thickness`~~ ✅ 2026-07-08（root `DecorationPaint` 贯穿 plain/rich/inline 三路径；style=solid/double/dotted/dashed/wavy）。
- [x] ~~`text-align:justify`~~ ✅ 2026-07-09（`JustifyExtra`/WordSpacing 通道，plain+inline 双路径；rich spans 不参与）。
- [x] ~~`<input type=submit/reset/button>`~~ ✅ 2026-07-08（渲染为按钮，submit/reset 联动 `<form>`）。
- [x] ~~`input type=file`（原生文件对话框）~~ ✅ 2026-07-09（`qui.OpenFile`/`OpenFiles`，`accept`/`multiple`；非 darwin no-op）。
- [x] ~~`input type=color`（应用内色板 Popup）~~ ✅ 2026-07-09（`widgets.Popup` 24 色网格；非原生取色面板）。
- [x] ~~`input type=date` 等格式提示 placeholder~~ ✅ 2026-07-09（`typeFormatHint`；非原生日期选择器）。
- [x] ~~flex 项 auto margin 吸收剩余空间~~ ✅ 2026-07-09（主轴吸收+压制 justify、交叉轴居中；四边 `Margin*Auto`→FlexLayout）。
- [x] ~~min/max 的 `%`~~ ✅ 2026-07-09（`Min/MaxWidthPct` 等→`resolvedMinSize/MaxSize`，FlowLayout block + flex item）。
- [ ] **表单细节**：select `multiple`、`datalist/output`、number 步进器、原生 date/color 选择器（需 NSDatePicker/NSColorPanel cgo 桥，未立项）。
- [ ] **文本引擎类残留**：`text-shadow`。
- [ ] **视觉细节**：`box-shadow` inset（无内阴影 primitive）、`backdrop-filter`（需背景采样）、`transform` 3D、`background-position/repeat`、repeating 渐变、远程图片/`srcset`。
- [ ] **杂项**：状态改字重字号（走 restyle）、`float/clear/columns`、`tabindex`、`calc()` 内含 `%`。

---

## 6. 测试约定（每个特性落地时照做）

| 目标 | 文件/命令 | 参考现有 |
|---|---|---|
| 级联 / 选择器正确性 | `selector_test.go` `engine_test.go` | 已有 |
| 计算值 / 简写展开 | `shorthand_test.go` `vars_test.go` | 已有 |
| hsl()/hsla() 颜色 + calc() 长度 | `values_test.go` | P2 |
| :nth-of-type / :checked / :disabled | `selector_test.go` | P2 |
| Flex align-self/shrink/basis/order | `flex_test.go` + root `layout_test.go` | P2 |
| :nth-last-child / :empty / :has | `selector_test.go` | P2 |
| Grid 显式放置 / template-areas | `grid_test.go` | P2 |
| input password/range + label[for] | `forms_test.go` | P2 |
| radial-gradient / bg url() / data: img | `visual_test.go` | P2 |
| input disabled attr / outline | `forms_test.go` | P2 |
| min()/max()/clamp() 长度 | `values_test.go` | P2 |
| visibility:hidden 保留布局 | `visibility_test.go` | P2 |
| transform skew/matrix/origin · box-shadow 多层 · z-index · @media · ::before/after · filter · conic | `visual_test.go` | P2/P3 |
| per-corner border-radius（简写/单角/椭圆/塌回统一） | `radius_test.go` + root `path_test.go`(`AddRRectCorners`) | 单角 |
| `:checked`/`:disabled` 运行时联动 + `<form>` 提交 | `form_live_test.go` | 动态 |
| letter-spacing/word-spacing/text-indent | `text_spacing_test.go` + root `text_layout_test.go`(`RuneAdvance`/`FirstIndent`) | 文本引擎 |
| 布局几何（flex/grid/position） | `grid_test.go` `position_test.go` | 已有 |
| 交互状态（盒装饰） | `hover_test.go` `button_hover_test.go` | 已有 |
| 祖先/兄弟状态触发（hover/focus/active·per-trigger payload） | `ancestor_hover_test.go` `ancestor_state_test.go` | 状态作用域 |
| 交互状态（文字色/下划线） | `hover_text_color_test.go` | P0.1 |
| 表单控件 radio | `radio_test.go` | P0.2 |
| 运行时 display:none | `display_none_test.go` | P0.3（含点击驱动 toggle + gap 无残留） |
| % 宽高 / margin:0 auto | `flow_block_size_test.go` + root `layout_flow_test.go` | 包含块参照 |
| flex 项 auto margin（主轴推/居中·交叉轴居中） | `flex_test.go`(`TestFlexAutoMargin*`) + root `layout_test.go`(`TestFlexAutoMargin*`) | A |
| min/max 的 `%`（max-width:100% 不溢出） | `flow_block_size_test.go`(`TestFlowMaxWidthPercentClamps`) + root `layout_flow_test.go`(`TestFlowLayoutPercentMinMax`)/`layout_test.go`(`TestFlexPercentMaxWidth`) | B |
| input file/color/date（原生对话框·色板·格式提示） | `forms_test.go`(`TestInputFile*`/`TestInputColorRendersAsSwatch`/`TestColorGridGeneration`/`TestInputDateFormatHint`/`TestAcceptExtensions`) | E |
| flex align-content | `flex_test.go` + root `layout_test.go`(`TestFlexWrapAlignContent`) | 多行分布 |
| 具名色全集 / 属性选择器 i 标志 | `values_test.go`(`TestNamedColors`) / `selector_test.go` | 抛光 |
| text-align:justify（plain+inline） | `text_justify_test.go` + root `text_layout_test.go`/`inline_test.go` | 两端对齐 |
| tracking 随 canvas scale（2× 屏） | root `text_tracking_scale_test.go` | Retina 回归 |
| justify 选区不超列宽 | `../widgets/label_test.go`(`TestLabelJustifySelectionStaysInColumn`) | 选区 clamp |
| white-space pre/pre-wrap/pre-line | `white_space_test.go` | P1 |
| overflow-wrap/word-break（plain+inline） | `text_overflow_wrap_test.go` + root `text_layout_test.go`/`inline_test.go`(`BreakLongWords`) | 长词断行 |
| 折叠链接 hover 变色 | `folded_link_hover_test.go` | P0.1 residual |
| 原子行内元素基线对齐 | root `inline_test.go` (`InlineBaseline`) | 修复 |
| box-sizing content/border-box | `box_sizing_test.go` | P1 |
| position:absolute 脱流+定位 | `position_absolute_test.go` | P1 |
| FlowLayout block 显式宽高 | `flow_block_size_test.go` | P1 |
| table 列对齐/colspan/条纹/拍平 | `table_test.go` | P1 |
| table caption/colgroup宽/fixed | `table_test.go` | P2(Phase 2) |
| table border-collapse/rowspan行高 | `table_test.go` + root `grid_test.go` | P2(Phase 2) |
| table 复制成 `<table>`/TSV | `../table_clipboard_test.go`(root) + `table_test.go`(coords) | 剪贴板 |
| 混排选区(富文本+列表+表格)/空格对齐 | `mixed_clipboard_test.go`(端到端) | 剪贴板边界 |
| 文本选择 / 剪贴板 | `inline_selection_test.go` `*_clipboard_test.go` | 已有 |
| reactive 实时 restyle 作用域 | `scoped_restyle_test.go` | 已有 |
| `::before/::after` `content:attr()` | `visual_test.go`(`TestBeforeAfterAttrContent`/`TestUnquoteContent`) | attr |
| `input type=submit/reset/button` 渲染+提交/重置 | `forms_test.go`(`TestInputButtonTypes*`/`TestInputSubmitAndReset`) | 表单按钮 |
| `text-decoration-color/style/thickness` | `text_decoration_test.go` + root `inline_test.go`(`TestInlineCarriesDecorationPaint`) | 文本装饰 |
| `app-region` 继承 + no-drag 挖洞 + 嵌套点击归属 | `app_region_test.go` + `nested_click_test.go` | 自绘标题栏 |
| `cursor`（关键字映射 / 继承 / auto 重置 / restyle 清除 / `a[href]` UA） | `cursor_test.go` + root `cursor_test.go`（两级优先、per-point、移出即复位） | P0 |
| `user-select`（继承 / opt-back-in / -webkit 别名 / restyle 复原） | `user_select_test.go` | P0 |
| `pointer-events`（继承 / auto 重开 / 穿透到底下的 widget / restyle） | `pointer_events_test.go` + root `pointer_events_test.go` | P0 |
| `readonly`/`maxlength`/`hidden` 属性 + 表单态伪类 | `form_attrs_test.go` | P0 |
| 事件（enter/leave·dblclick·wheel 消费语义·key 消费+focusable·focus/blur·disabled 吞键） | `events_test.go` | P0 |
| `<option value/selected/disabled>` · optgroup 标题 · color 提交值 | `select_options_test.go` | P1 |
| select type-ahead（前缀/连敲循环/超时/无匹配回退/跳过 disabled/Cmd 不吃） | `../widgets/select_typeahead_test.go` | P1 |
| `type=search` 清空按钮（显隐/占位/点击/覆盖/**真实像素**） | `../widgets/input_trailing_test.go` + `../widgets/input_clear_paint_test.go` | P1 |
| `<datalist>` 补全（解析/过滤/开合/键盘导航/Enter 回落/onInput 只一次） | `datalist_test.go` | P1 |
| `type=number` 步进器（step/默认1/clamp/空值起步/onInput/纯文本无步进） | `number_step_test.go` | P1 |
| 视觉回归（PNG） | `QUI_HTMLCSS_SNAPSHOT=1 go test ./htmlcss` → `/tmp/qui-htmlcss/page.png` | `snapshot_test.go` |
| 全量 | `go test ./htmlcss ./reactive/...` | —— |

**改动 backing widgets（Box/Label/InlineBox/Input/TextArea/CheckBox/Select/ScrollView/Image/Rule）后必跑
`go test ./htmlcss ./reactive/...`** —— 它们是 htmlcss 的稳定契约（见 CLAUDE.md「backing-primitives contract」）。

**离屏快照是 1× 缩放的**：凡涉及 canvas scale 的特性（tracking、逐字形绘制、任何"测量在逻辑单位、绘制在物理单位"的通道），快照验收暴露不了 2× 屏的度量/绘制失配——要么写 2× 画布测试（参照 root `text_tracking_scale_test.go`：1×/2× 画同文案比 ink 边缘），要么真机验收。
