# htmlcss —— 能力矩阵

这是 `htmlcss` 引擎的权威能力清单。它回答：支持什么、哪些明确留给原生控件、边界在哪里。

> qui 不是一个完整的浏览器。目标是「**轻量 HTML5 + CSS 子集**」，覆盖应用开发中真正高频的排版、样式与交互；其余能力（媒体、画布、高级表格布局等）建议直接用原生 `widgets` 或专门的包。

**图例：** ✅ 完整 · 🟡 部分（见备注）· ❌ 未实现 · 🚫 设计上不支持（走原生 widgets / 其他包）。

---

## 1. HTML 标签

### 结构 / 文本 / 语义

| 标签 | 状态 | 备注 |
|---|---|---|
| `html` `head` `body` | ✅ | head 及其内容不渲染；body 为根 |
| `div` `section` `article` `header` `footer` `nav` `main` `aside` | ✅ | 通用 block 容器 |
| `span` | ✅ | inline，折进文本 run |
| `p` `h1`–`h6` | ✅ | 带 UA 默认 margin / font-size |
| `strong` `b` `em` `i` `small` `u` `s` `del` `ins` `strike` `mark` `abbr` `sub` `sup` | ✅ | inline，折叠为带样式 span |
| `code` `pre` | ✅ | monospace；`<pre>` / `white-space:pre` 保留空白与换行 |
| `a` | ✅ | href 点击调用 `OpenURL`；折叠 span 里也可点（`InlineBox.LinkAt`） |
| `br` | ✅ | inline 强制换行 |
| `blockquote` | ✅ | UA 左边框 + 缩进 |
| `hr` | ✅ | 渲染为 `widgets.Rule`；`border-*` 作用于线本身 |
| `ul` `ol` `li` | ✅ | 悬挂 `[marker\|content]` 行；disc/circle/square/decimal；ol 自动编号；剪贴板可重建列表结构 |
| `img` | 🟡 | 本地光栅 + `.svg`（可染色）+ `data:` URI + `object-fit: cover/contain`；**不支持**远程 URL / `srcset` |
| `label` | 🟡 | 折叠为 inline 文本；`for=` 点击联动见下方 `label[for]` 行 |
| `table` `thead` `tbody` `tfoot` `tr` `td` `th` `caption` `colgroup` `col` | ✅ | 拍平成一个 `GridLayout`：列跨行对齐、内容自适应列宽（有 width 时等分 1fr）、`colspan`、`th` 粗体居中、`tr:nth-child` 行条纹、`border-spacing`、auto-width 表 shrink-to-fit。另有：`<caption>` 全宽跨列行（`caption-side:top/bottom`）、`<colgroup>/<col>` 列宽（`width` 属性或内联 `style`→`GridFixedTrack`，支持 `span`、% 参照表宽）、`table-layout:fixed`、`border-collapse:collapse`（单线合并）、`rowspan` 行高计入。限制：`<col>` 不吃 class 选择器；内容比例分配列宽未做 |
| `dl` `dt` `dd` `figure` `figcaption` | ✅ | UA 默认样式：dd/figure 缩进、dt 粗体、figcaption 弱化 |
| `script` `style` `meta` `link` `title` | 🚫 | 不渲染；`<style>` 内容会被采集进样式表 |

### 表单控件（经 backing widget 渲染）

| 标签 | 状态 | 备注 |
|---|---|---|
| `input[type=text]`（默认） | ✅ | `widgets.Input`；placeholder / value / onInput / onSubmit |
| `input[type=checkbox]` | ✅ | `widgets.CheckBox`；onToggle；可选 value 标签 |
| `input[type=checkbox switch]` | ✅ | Safari 式 `switch` 布尔属性 → `widgets.Switch`；checkbox 语义（`checked`/`:checked`/表单序列化一致）；`accent-color` 染 on 轨道 |
| `input[type=radio]` | ✅ | 真 `RadioButton`，按 `name` 分组单选；`value` 为可见标签，`checked` 为初选 |
| `input[type=password]` | ✅ | 掩码显示（每字符 •），真值保留，caret / 选区按掩码测量 |
| `input[type=range]` | ✅ | `widgets.Slider`；min/max/value/step；onChange 回传数值字符串 |
| `input[type=number/email/search/tel/url]` | 🟡 | 渲染为普通文本框。`type=search` 有 UA 清空按钮（有内容时右侧 ✕，点击清空并触发 input、不触发 submit）。`type=number` 有步进器（按 `step` 缺省 1 增减、clamp 进 `[min,max]`、经 onInput 上报）。**欠**：类型校验（`:valid`/`:invalid`） |
| `readonly` / `maxlength` 属性 | ✅ | 经 backing widget 的 `BeforeInput` 否决钩子实现，每条改动路径都过这个闸。readonly 字段仍可聚焦 / 选中 / 复制。`maxlength` 按 rune 计数；删除永不拦；替换选区可释放其长度 |
| `input[type=date/time/datetime-local/month/week]` | 🟡 | 文本框 + 格式提示 placeholder（date→`YYYY-MM-DD`、time→`HH:MM` 等）；**无**原生日期选择器 / 校验 |
| `input[type=color]` | 🟡 | 渲染为填充色块按钮；点击弹应用内色板 `Popup`（None 行 + 灰阶行 + 10 色相矩阵 + 手写 hex 输入带实时预览）。弹出位置自适应（默认下方、放不下翻上、越界 clamp）。交互态只改边框环、不改填充。**无**原生取色面板 / 自由取色 |
| `input[type=submit/reset/button]` | ✅ | 渲染为按钮（Box + 文字，label 取 `value`，缺省 Submit/Reset），借用 `<button>` UA chrome。submit 提交上级 `<form>`、reset 恢复各控件默认、button 仅 author onClick。表单序列化里不算成功控件 |
| `input[type=file]` | ✅ | 渲染为按钮；点击弹原生文件对话框（`qui.OpenFile`/`OpenFiles`；`multiple`；`accept` 的 `.ext` 令牌→`AllowedExtensions`）。label 为所选文件基名（多选加 `(+N)`），缺省 `Choose File…`；序列化回传绝对路径。非 darwin 返回 `ErrDialogNotSupported` |
| `textarea` | ✅ | `widgets.TextArea` |
| `select` `option` `optgroup` | ✅ | 需 `Options.Window`；无 window 时降级为占位 label。`option value` 是提交值（缺省回退到可见文字）、`selected` 设初选且被 form reset 还原、`disabled` 被点击 / 方向键 / type-ahead 跳过；`<optgroup>` 在其选项前插一条不可点的分组标题行。键盘 type-ahead（连敲同一字母循环、1s 无输入清空缓冲）。限制：分组是「标题行 + 平铺」的近似；无 `multiple`/`size` |
| `button` | ✅ | Box 渲染 + 内建 hover/press 反馈。可以 Tab 到达，Enter / Space 按下（推按钮型 `<input>` 也一样）。鼠标点击不转移焦点（输入框保持焦点），除非作者写了 `:focus` 样式。不画内建焦点环，需要的话自己写 `:focus` |
| `form` | ✅ | `El.SetOnFormSubmit(func(map[string]string))` 收集具名后代控件的当前值。文本框回车或 submit 按钮触发；`<input type=reset>` 恢复默认。限制：无原生 action/method 网络提交、无 `formdata`/校验 |
| `fieldset` `legend` | ✅ | UA 边框 / 内距 + legend 粗体 |
| `label[for]` | ✅ | 点击激活关联控件（勾选 checkbox / 选中 radio / 聚焦文本框）。作为控件面，其文字不可拖选；点击只 toggle |
| `datalist` | ✅ | `<input list=ID>` 的原生补全：输入即过滤（大小写不敏感子串，同 Chrome）、非模态 `Popup` 锚在字段下方（字段保持焦点）、方向键移动、Enter 确认、Esc 关闭。导航键在 **capture 阶段**截获，避免确认建议的 Enter 被当作表单提交。`<datalist>` 自身不渲染。程序式入口 `El.SetSuggestions` / `h.Suggest`。已知差异：建议表按最宽项自适应，而非与字段等宽 |
| `output` | ❌ | 未实现 |

### 替换元素 / 自绘挂载点

| 标签 | 状态 | 备注 |
|---|---|---|
| `canvas` | ✅ | 「widget 自绘」挂载点（不给 JS 2D context，直接用 qui 的绘制能力）。`El.SetCanvasDraw(func(cv qui.Canvas, bounds qui.Rect))` 装一个每帧调用的绘制回调；`El.SetCanvas(qui.Widget)` 插入任意自定义 widget（`scene3d.Viewport`、`graphs` 图表……）。CSS `width/height` 定尺寸（缺省 300×150）；未接线时渲染虚线占位框。默认 `display:inline-block`、`Role()=image` |

### 明确不支持（🚫 走原生 widgets / 其他包）

| 标签 | 替代方案 |
|---|---|
| `audio` | `media.AudioPlayer` |
| `video` | `media.VideoView` |
| `iframe` / 内嵌浏览器 | `webview`（CEF，需 build tag） |
| `svg`（内联标签） | `<img src=*.svg>` 或 `h.Icon` + `svg.Document`（`qui.VectorSource`） |
| `dialog` | reactive `h.Dialog`（外壳）/ `h.ModalPortalWith` / `widgets.Dialog`。portal 内容从声明它的元素继承样式，包括属性继承和祖先选择器 |
| `details/summary` `progress` `meter` | 原生 `widgets` 组合（ScrollView/Progress 等） |
| `text-shadow` | ❌ |

---

## 2. CSS 属性

### 文本 / 字体（可继承）

| 属性 | 状态 | 备注 |
|---|---|---|
| `color` | ✅ | 也推到表单控件文字色；仅显式声明时覆盖 UA 默认 |
| `accent-color` | ✅ | 继承；驱动控件 active chrome（checkbox 勾选框 / radio 圆点 / switch 轨道 / slider active track+handle+state layer）。业务暗色主题以 CSS 表达时的关键属性 |
| `font-size` | ✅ | px/em/rem/pt/% |
| `font-weight` | ✅ | bold/normal/lighter + 数值 |
| `font-style` | ✅ | italic |
| `font-family` | ✅ | 取第一个 family |
| `font`（简写） | ✅ | `expandFont` |
| `line-height` | ✅ | 无单位倍数 / 长度 |
| `text-align` | ✅ | start/center/end/**justify**。justify 只拉伸软换行行；经 root `JustifyExtra` + `Font.WordSpacing` 通道贯穿 plain（Label）与 inline（InlineBox）两条路径，绘制 / 选择 / 命中一致。富文本 spans 不参与 |
| `text-decoration` / `-line` | ✅ | underline / line-through / overline + `-color` + `-style`（solid/double/dotted/dashed/wavy）+ `-thickness`。root `DecorationPaint` 贯穿 plain / rich / inline 三路径。限制：wavy 用 zig-zag path 近似 |
| `text-transform` | ✅ | upper/lower/capitalize |
| `overflow-wrap` / `word-wrap` / `word-break` | ✅ | `break-word`/`anywhere`/`break-all` 打开长词内断行（贴 URL、无空格的数字/CJK 长串）；`normal`/`keep-all` 溢出。两条文本装配同时生效（plain 与 inline），切点落在 grapheme 边界、caret 可穿过。限制：`break-all` 按 `break-word` 近似；`hyphens` 未实现 |
| `text-overflow: ellipsis` | 🟡 | 仅配合 `white-space:nowrap` 单行 |
| `white-space` | ✅ | normal / nowrap / `pre` / `pre-wrap` / `pre-line`。限制：`<pre>` 内混排行内子元素的空白仍走折叠 |
| `letter-spacing` `word-spacing` | ✅ | 经 `Font.LetterSpacing`/`WordSpacing`；`normal` 归零；均继承；tracking 随 canvas scale 缩放。限制：spacing 路径下合成斜体 shear 退化 |
| `text-indent` | ✅ | 首行缩进（`ParagraphStyle.FirstIndent`）。限制：与 `text-align:center/end` 且无界宽度组合时缩进被对齐重算覆盖 |

### 盒模型 / 边框

| 属性 | 状态 | 备注 |
|---|---|---|
| `width` `height` `min-*` `max-*` | 🟡 | px/em/rem/pt/`calc()`；常规流 block 认显式 width/height（左对齐、不撑满；min/max clamp）+ flex/grid/inline-block。`%` 参照包含块在布局期解析（含 min/max，如 `max-width:100%` 防溢出），按 border-box 计。**欠**：`calc()` 内含 `%`、inline-block / 绝对定位的 `%` |
| `padding` `margin`（简写 + 单边） | ✅ | 1–4 值规则；CSS margin-box 语义（Flex/Flow）；`margin: 0 auto` 水平居中；flex 项 auto margin。**欠**：flex 项 auto margin 在 overflow 下按 0（合规） |
| `border` / `border-width/color/style` | ✅ | `border: none` 显式声明会清除控件的原生边框（`HasBorder` 区分「声明了 none」与「未声明」） |
| `border-<side>*` 单边 | ✅ | |
| `border-style` | 🟡 | solid/dashed/dotted/none；double/groove/ridge/inset/outset 退化为 solid |
| `border-radius` | ✅ | 统一半径 + 单角 + 1–4 值简写 + 椭圆 `a / b`；`overflow:hidden` 按圆角裁子元素。限制：box-shadow 仍按统一 `Radius` |
| `box-sizing` | ✅ | 默认 `content-box`，支持 `border-box`。border 画在内、不额外占布局，故 content 面积可能差 border 厚度 |
| `outline` | ✅ | `outline`/`-width`/`-color` → 边框外描一圈（不占布局）；无 `outline-offset`/style |

### 背景 / 视觉效果

| 属性 | 状态 | 备注 |
|---|---|---|
| `background` / `background-color` | ✅ | 简写按颜色解析 |
| `background-image: linear-gradient()` | ✅ | 角度 + `to <side>`；多色标 |
| `background-image: url()` | 🟡 | 位图经 `qui.ImageShader` 拉伸铺满（含 `data:` URI）；**无** `background-size`/`position`/`repeat`（恒 100% 拉伸） |
| `radial-gradient` | ✅ | 忽略 shape/size/position 前缀，恒 farthest-corner；多色标 |
| `conic-gradient` | ✅ | `from <angle>` + 多色标（`qui.ConicGradient`）；position 忽略（居中） |
| `repeating-*` gradient | ❌ | |
| `background-size` | 🟡 | `cover`/`contain`/拉伸（`qui.ImageFit`）；无显式尺寸 / `position` / `repeat` |
| `box-shadow` | 🟡 | 多层（逗号分隔）；**inset 忽略**（无内阴影 primitive） |
| `opacity` | ✅ | |
| `transform` | ✅ | translate/rotate/scale/skew/matrix（2D，经 `Canvas.Concat`）+ `transform-origin`（关键字/%）；**无** 3D |
| `filter` | 🟡 | `blur()` + `drop-shadow()` 经 SaveLayer；多函数取首个；无 brightness/contrast |
| `backdrop-filter` | ❌ | 需背景采样 |

### 布局 / 定位

| 属性 | 状态 | 备注 |
|---|---|---|
| `display: block/inline/inline-block/none/flex/grid` | ✅ | 静态 `none` 编译期剪除；运行时切换 `none` 也生效且父布局彻底跳过该盒（无 flex/grid gap 残留） |
| Flex：`flex-direction/justify-content/align-items/flex-wrap/gap/flex-grow` | ✅ | `align-items:stretch`（默认）只拉伸交叉轴为 auto 的项；显式 `width`（column）/`height`（row），含百分比，保持自己的尺寸并贴行首，同浏览器 |
| Flex：`align-self/flex-shrink/flex-basis/order` | ✅ | `flex` 简写展开 grow/shrink/basis（none/auto/initial）；`order` 稳定排序；`flex-shrink:0`→NoShrink |
| Flex：`align-content` | ✅ | wrap 多行在剩余交叉轴空间的分布：start/center/end/space-between/around/evenly/stretch |
| Grid：`grid-template-columns/rows`（px/fr/auto/repeat）、`gap`/`row-gap`/`column-gap` | ✅ | |
| Grid 单元格放置：`grid-column/row`、`span`、`grid-template-areas` | ✅ | 行/列线号（1-based `a / b`）、`span N`、`grid-area`（命名区或 4 线号）；命名区 + 隐含轨道数。限制：仅 `grid-column/row` 简写（不读 `*-start`/`*-end`）；混合「显式一轴 + 自动另一轴」退化为纯 span 自动排布 |
| `position: relative` | ✅ | top/right/bottom/left 偏移；建立包含块 |
| `position: absolute` | ✅ | 脱离常规流，相对最近 positioned 祖先定位。限制：包含块用 border box 近似；无 `z-index` 层叠；`auto` 侧落在包含块原点而非静态位 |
| `position: fixed` | 🟡 | 当作 absolute（相对最近 positioned 祖先 / root）；**不随视口固定** |
| `position: sticky` | ❌ | |
| `overflow: hidden/clip/auto/scroll`（含 `-x`/`-y`） | ✅ | auto/scroll 托管 `ScrollView` |
| `visibility: hidden/collapse` | ✅ | 保留布局盒、不绘制（自身 + 子树）。限制：子孙 `visibility:visible` 反显未支持 |
| `z-index` | 🟡 | 绘制顺序按 z 升序；**无完整 stacking context** |
| `float` `clear` `columns` | ❌ | 无浮动 / 多列 |

### 交互 / 指针

| 属性 | 状态 | 备注 |
|---|---|---|
| `cursor` | ✅ | 继承；root 层两级声明式解析 —— 显式声明（CSS / `SetCursorShape`）由内向外胜出，否则用 widget 内建形状（链接手型、文字 I 形、Anchor 手型、tooltip）。UA 给 `a[href]` 加 `pointer`；`<button>` 不加（浏览器行为）。限制：GLFW 3.3 只有 6 种标准光标，`not-allowed`/`move`/`grab`/`wait`/`zoom-*`/斜向 resize 退化成箭头；不支持 `url()` 自定义光标 |
| `user-select` | ✅ | 继承（含 `-webkit-` 别名）。`none` 把元素文字移出拖选与剪贴板；`text`/`auto`/`all`/`contain` 复原。控件面即使没声明也不可选。双向：撤掉规则会交还可选性 |
| `pointer-events: none / auto` | ✅ | 继承；在选目标处过滤，所以 hover/focus/tooltip/cursor/drag 看到同一目标。透明元素自己不当目标，但先下钻子元素，于是 `auto` 后代能重新开洞（浏览器行为）。SVG 专用值按「可命中」处理 |

### 选择器 / 变量 / at-rules

| 特性 | 状态 | 备注 |
|---|---|---|
| 标签 / `.class` / `#id` / `*` | ✅ | |
| 属性选择器 `[a]` `=` `^=` `$=` `*=` `~=` `\|=` | ✅ | 支持大小写不敏感标志 `i` |
| 组合器（后代 / `>` / `+` / `~`） | ✅ | 右到左匹配 |
| `:hover` `:focus` `:active` | ✅ | `focus-within`/`visible` 归一为 focus |
| 祖先/兄弟状态触发后代（`.row:hover .del`、`.a:hover ~ .b` 等） | ✅ | 由选择器点名的那个元素的状态驱动盒装饰 / 文字色 / `visibility`。触发器精确发现；payload 按当前活跃触发器组合计算。限制：`display:none`→揭示未支持；一条规则带两个非主体状态 compound 不发现触发器 |
| `:root` `:first-child` `:last-child` `:only-child` `:nth-child(An+B)` `:not(simple)` | ✅ | |
| `:nth-of-type(An+B)` | ✅ | 按同 tag 计数 |
| `:checked` `:disabled` | ✅ | 属性驱动 + 运行时实时联动：勾选 / 切换单选或 `El.SetDisabled(bool)` 会同步属性并触发作用域 restyle，使 `:checked`/`:disabled`（含 `:has(:checked)` 等）即时重级联 |
| `:enabled` `:required` `:optional` `:read-only` `:read-write` | ✅ | 属性驱动。`:enabled`/`:required`/`:optional` 只作用于表单控件；`:read-only` 按规范匹配一切非用户可改元素；`:read-write` 只匹配可编辑控件。**欠**：`:valid`/`:invalid`/`:placeholder-shown`/`:indeterminate`/`:default` |
| `:nth-last-child(An+B)` `:empty` `:has()` | ✅ | `:has()` 限单个简单后代选择器（`div:has(img)`） |
| `::before` `::after` | 🟡 | 生成 `content` 文本（叶 / 行内元素折进同一 InlineBox）。`content` 支持引号字符串 + `attr(name)` + 拼接。限制：带块级子元素的元素上不生成；不支持 `counter()` 或生成盒的完整盒模型 |
| 其他伪元素（`::first-line`…） | ❌ | 解析但不生成 |
| CSS 变量 `--x` / `var(--x, fallback)` / `:root` | ✅ | 继承 + 递归解析（深度上限 16） |
| 简写 `font` `flex` `inset` | ✅ | |
| `@media` | 🟡 | `min-width`/`max-width`（`and`、screen/all/print）在解析时对视口宽度求值；匹配块扁平进样式表。**非响应式**（不随 resize 重算） |
| `@font-face` `@keyframes` `@import` `@supports` | ❌ | at-rule 整块跳过 |
| `!important` / 级联优先级 / 内联 style | ✅ | UA < author < inline，important 跨层 |
| 运行时换样式表 `StyleEngine.SetCSS` | ✅ | 重解析并全量 Restyle，树原地重级联 —— 主题切换钩子（换一套 `:root` 变量即可）。`RenderResult.Engine` 暴露静态渲染的引擎 |
| 颜色：hex 3/6/8、`rgb()`/`rgba()`、`hsl()`/`hsla()`、具名色全集 | ✅ | hsl 支持逗号式与空格式、`deg`/负 hue；CSS Level 4 具名色全集 148 色 + transparent。无 `hwb()`/`lab()`/`lch()` |
| 长度：px/em/rem/pt/%/无单位/`calc()`/`min()`/`max()`/`clamp()` | 🟡 | `calc()` 支持 `+ - * /`、括号、嵌套、单位混算；`min`/`max`/`clamp` 各参数经 `parseLength`。限制：内含 `%` 只在该属性本身支持 `%` 参照时有效。**无** `vw`/`vh`/`ch` |
| `transition` / `animation` | ❌ | 动态基建未建 |

---

## 3. 事件与交互

| 能力 | 状态 | 备注 |
|---|---|---|
| 点击 `onClick` | ✅ | |
| 右键 `onContextMenu` | ✅ | 回传窗口坐标，可锚定菜单 |
| `<a href>` 导航 | ✅ | 元素本身或折叠 span 均可点 |
| `:hover`/`:active` 盒装饰 | ✅ | button 无 author 规则时有内建 darken/press |
| `:hover`/`:focus`/`:active` 改文字色 + text-decoration | ✅ | 独立元素与折叠行内链接均可。限制：状态改 `font-weight`/`size` 会重排，未在 draw 时应用 |
| `:focus` 盒样式 | ✅ | focusable 盒子会禁用子 Label 选择以抢焦点 |
| 输入 `onInput` / 回车 `onSubmit` | ✅ | input/textarea |
| checkbox `onToggle` / select `onChange` | ✅ | 用户切换会同步 `checked` 属性 + relink `:checked` |
| `<form>` 提交 `onFormSubmit` | ✅ | `SetOnFormSubmit(map[string]string)`；回车 / submit 按钮触发，收集具名控件值 |
| `onMouseEnter` / `onMouseLeave` | ✅ | 每次进出各触发一次（窗口按命中路径 diff 合成，DOM mouseenter/leave 语义、不冒泡） |
| `onDoubleClick` | ✅ | 两次左键释放在 400ms + 5px 内合成；顺序 click→click→dblclick |
| `onWheel` | ✅ | 收 `(dx, dy)`；返回 true 才消费，否则外层 ScrollView 继续滚动（等价于不调 preventDefault）。冒泡阶段，内层优先 |
| `onKeyDown` / `onKeyUp` | ✅ | 收 `qui.KeyEvent`，返回 true 消费。元素自己持有焦点（声明 key handler 即变可聚焦），或事件从聚焦的后代冒泡上来。disabled 元素吞键 |
| `onFocus` / `onBlur` | ✅ | 在焦点跃变上各触发一次 |
| 跨 widget 文本选择 | ✅ | Label + InlineBox 实现 `TextSelectable` |
| 剪贴板复制（纯文本 + HTML flavor） | ✅ | 图片内联 data URI；列表重建 `<ol>/<ul>`；表格重建 `<table>`（HTML→Docs/Word 真表格；plain→TSV 供 Sheets）。混排选区各自成结构、按文档顺序。选区坐标归一到包围盒。限制：单元格内嵌套列表 flatten 成 `<br>`；整块选区里跨行/跨列合并单元格为近似 |
| 拖拽重排 Drag & Drop | ✅ | Draggable/DragHandle/OnDrop/OnDragOver，paint-only Transform 反馈 |
| `app-region: drag / no-drag` | ✅ | 可继承（Electron `-webkit-app-region` 别名）；`drag` 声明在标题栏条上，整个子树可拖窗，`no-drag` 子元素挖回来。落到 `Window.BeginWindowDrag()`。只在 `Window.SetTitlebarStyle(qui.TitlebarOverlay)` 的窗口有意义；不支持的平台退化为普通按下 |
| 键盘焦点 / Tab 循环 | ✅ | 框架层；控件编辑走 backing widget。可选中的文字能点击聚焦（用于复制），但不是 Tab 停靠点。`El.RequestFocus()` 可以用代码设置焦点 |
| HTML `disabled` 属性 | ✅ | 禁用 backing 控件；`El.SetDisabled(bool)` 运行时切换并 relink `:disabled` |
| HTML `title` 属性 → hover tooltip | ✅ | restyle 时推到 widget `SetTooltip`；折叠进行内元素无独立 widget |
| HTML `hidden` 属性 | ✅ | 按 UA 规则实现（tier 0 的 `display:none`），作者 CSS 可覆盖。静态 `Render` 下命中 display:none 的编译期剪枝 |
| `autofocus` | ✅ | 元素首次挂载时聚焦一次，表单元素会聚焦到它的编辑控件；在 portal 里同样有效 |
| `tabindex` / `accesskey` / `minlength` / `pattern` / `rows` / `cols` / `inputmode` / `spellcheck` | ❌ | 不读取 |
| `label[for]` 点击聚焦控件 | ✅ | toggle / select / focus |
| AX 角色（button/link/textbox/img/list/heading…） | ✅ | `El.Role()` + `AccessibleName()`，agent 可寻址 |

---

## 4. 已知缺口

按桌面应用需要它的高频程度分组。

**高价值（原生 + 日常）：** `aspect-ratio`；`position: sticky`；`::placeholder` 与 `:placeholder-shown`/`:valid`/`:invalid`/`:indeterminate`/`:default`（共用「值变化 → 作用域 restyle」实时钩子）；`::selection`/`::marker`；`:is()`/`:where()`/CSS 嵌套；`transition`；其余鼠标事件（`mousemove`、`scroll`、`change`）；`tabindex`/`minlength`/`pattern`/`rows`/`cols`/`inputmode`/`spellcheck` 属性。

**中价值：** Grid `justify-items`/`justify-self`/`place-*`/`grid-auto-*`/隐式轨道；`min-content`/`max-content`/`fit-content` 尺寸关键字；`@media` 响应式重算与 `prefers-color-scheme`/`(hover)`/`(pointer)`/`prefers-reduced-motion`；`@container`；`color-scheme`；`oklch()`/`color-mix()`/`light-dark()`/`hwb()`/`lab()`；`text-shadow`；文本抛光（`text-wrap`、`hyphens`、`tab-size`、`font-variant*`、`font-feature-settings`、`text-underline-offset`）；`background-position`/`-repeat`/显式 `background-size`/repeating 渐变；远程图片 URL / `srcset`；滚动与 resize 抛光（`resize`、`appearance`、`caret-color`、`scrollbar-*`、`overscroll-behavior`、`scroll-behavior`、scroll-snap）；`<select multiple>`/`size`；原生日期 / 颜色选择器；`<a target>`/`download`。

**明确不做：** `float`/`clear`；多列；`@keyframes` + `animation`；`writing-mode`；`clip-path`/`mask`；`mix-blend-mode`/`isolation`；完整 stacking context；`backdrop-filter`；3D `transform`；`contenteditable`；`<iframe>`（走 `webview`）；Shadow DOM / `<template>` / `<slot>`；`content-visibility`/`will-change`；`@import`；`@supports`。

---

## 5. 后备控件契约

htmlcss 通过 `widgets.Box`、`Label`、`InlineBox`、`Rule`、`Input`、`TextArea`、`CheckBox`、`Select`、`ScrollView`、`Image` 渲染（`reactive/html` 还会用 `MenuItem`/`ShowContextMenu`）。这些 API 是稳定契约 —— 改动会悄悄破坏 CSS 渲染。改动后运行 `go test ./htmlcss ./reactive/...`。
