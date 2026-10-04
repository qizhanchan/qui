# qui —— 概览

[English](overview.md) · **中文**

## qui 是什么

`qui` 是一个保留模式（retained-mode）的 Go GUI 框架。它用一棵持久存在的控件树来构建桌面应用，并用自带的光栅化器渲染：默认是纯 Go 的 CPU 参考后端，此外还有一个通过 `QUI_GPU_RASTER=1` 开启的可选 GPU 后端。唯一的系统依赖是 GLFW，以及用于窗口与呈现层的 OpenGL 3.3 上下文。

整个项目是一个 Go module：`github.com/qizhanchan/qui`。根目录的 `package qui` 是引擎；其他一切都是可选的子包。

用 qui 构建界面有两种方式：

1. **原生控件** —— 在布局与容器原语中组合 `widgets.Button`、`widgets.Input`、`widgets.ListView` 等。直接、易调试、没有翻译层。
2. **HTML + CSS + Go** —— 用轻量标记与样式表描述界面（`htmlcss`），再用声明式运行时驱动它（`reactive` + `reactive/html`）。这是战略方向：它给应用开发者一个熟悉、高层、对模型友好的界面，而框架底层始终是一套保留模式控件引擎。

两条路径都编译到同一棵控件树，因此可以自由混用。

## 它要解决的问题

大多数 Go GUI 方案处于两个极端之一。即时模式库小而简单，但把状态管理、布局和样式都推给用户代码。原生工具包绑定能力强大，却把应用锁死在某个平台的 API 上，并要求开发者学习庞大而命令式的对象模型。

qui 选择保留模式的立场 —— 声明一棵稳定的树，让框架负责输入路由、布局、失效与绘制 —— 但：

- **核心保持小巧、依赖轻量**，把图表、SVG、3D、媒体、物理和国际化做成可选包；
- **用 HTML + CSS 作为高层应用界面**，让最常见的界面工作变成标记与样式，而不是命令式 Go；
- **把 AI 可操作性当作引擎的一等特性**，而不是附加项：每个应用开箱即可被语义选择器检查和驱动。

## 设计原则

**保留模式，失效驱动。** 没有失效就不绘制。`Invalidate` / `InvalidateRect` 处理纯视觉变化；`InvalidateLayout` 处理影响尺寸的变化，作用域限定在调用者的绘制范围，只有当某个控件的包围盒真的移动时才提升为全量重绘。这让空闲帧几乎没有成本，也让 `WaitIdle` 能对持续更新的应用工作。

**严格的依赖方向。** 子包 import 根引擎；根引擎绝不 import 子包。耦合通过根包定义的小接口进行（`qui.Animator`、`qui.IMEClient`、`qui.VectorSource`、`qui.Translator`）。`widgets` 除根包外什么都不 import；`reactive` 只 import 根包，与后端无关。这保持了引擎与可选特性的独立，并防止依赖成环。

**CSS 是样式层。** 引擎只提供一套浅色的设计 token 主题，刻意与具体设计系统无关。有设计感的外观属于样式表，而不是 token 包。

**延迟解析。** 因为树是保留的，构造时捕获的值会被冻结。因此文字、译文和消息 key 都在 Measure/Draw 中解析（`data-i18n` 在重样式时解析），一次语言切换只需一次重新布局，而不是重建。

**AI 原生。** 无障碍树、选择器语法、截图、动作、等待和事件录制都位于根引擎，始终可用。`agent` 包只是把它们通过 HTTP 暴露出来。

**一套配色，不捆绑设计系统。** qui 不是浏览器，也不是某个控件库的克隆；`htmlcss` 是面向应用布局而非文档的、有意为之的子集。

## 架构一览

```
                 应用代码
        ┌──────────────┴──────────────┐
        │                             │
   原生 widgets                htmlcss + reactive/html
   （widgets 包）              （标记 + CSS + Go DSL）
        │                             │
        └──────────────┬──────────────┘
                       │
              根 qui 引擎
   帧循环 · 事件分发 · 布局引擎
   Canvas / RasterBackend · 文字整形 · 主题
   locale 接缝 · AI 内省
                       │
             平台接缝（platform.go）
          ┌────────────┴────────────┐
       GLFW 后端                 Cocoa 后端
```

可选包 —— `scene3d`、`graphs`、`svg`、`media`、`webview`、`physics`、`anim`、`i18n`、`icons`、`fonts` —— 通过根包接口接入，引擎永远不会反过来依赖它们。

## 平台支持与状态

- **macOS** 是完整支持的平台。GLFW 后端与原生 AppKit（`cocoa`）后端都编译进来，在运行时选择；两者几何结果一致。
- **Linux / Windows** 可以编译并运行核心。原生对话框、IME、emoji 与系统托盘目前仅支持 macOS；平台接缝会返回 `ErrNotSupported`，而不是静默失败。
- **渲染**默认走 CPU 光栅器。GPU 后端需显式开启，并按图元与 CPU 路径交替使用。
- **文字**由 `go-text/typesetting` 整形（OpenType GSUB/GPOS、Unicode bidi、UAX #14 / #29），测量、绘制、PDF 输出、换行、命中测试、选择和编辑共享同一套结果。
- **国际化**支持消息目录、CLDR 复数、区域敏感格式化与完整 bidi 文字。布局镜像（RTL）尚未实现。

## 接下来读什么

- [architecture.zh.md](architecture.zh.md) —— 引擎内部的运作方式。
- [features.zh.md](features.zh.md) —— 功能清单。
- [../README.zh.md](../README.zh.md) —— 包地图与快速开始。
- [../reactive/README.zh.md](../reactive/README.zh.md) 与 [../htmlcss/COVERAGE.zh.md](../htmlcss/COVERAGE.zh.md) —— 应用开发界面的深入解析。
