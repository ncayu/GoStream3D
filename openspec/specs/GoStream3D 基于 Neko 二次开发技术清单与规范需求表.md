# GoStream3D 基于 Neko 二次开发技术清单与规范需求表

---

## 1. 项目定位与二次开发架构

* **项目名称**：`GoStream3D`
* **产品目标（当前实现的最高优先级）**：服务端浏览器提前打开并完成 3D 场景加载，在服务端使用 CPU 或 GPU 执行 WebGL 渲染；观看端只解码并显示服务端传来的画面，同时把鼠标、触摸、键盘输入传回服务端。观看端不得重新加载原始 3D 页面、下载其模型/纹理或在本机执行该页面的 WebGL 场景渲染。
* **CPU 画质策略（必须）**：始终保持链接配置的原分辨率和 1:1 渲染像素比例，不得在拖动、缩放或高负载时自动降低视口、截图尺寸或像素比例。CPU 算力不足时允许实际帧率低于配置目标，客户端应显示实际显示帧率。现有 CPU 硬配额继续生效，内存与磁盘缓存用于保留场景、资源和最新画面，不能作为保证复杂 3D 场景达到 30/60 FPS 的依据。
* **CPU 特效简化（2026-10-01 已授权）**：用户明确同意在原分辨率下简化阴影、描边等高耗时效果。Windows 本机 CPU 模式可选择 `GOSTREAM3D_CPU_EFFECTS=simplified`，在 WebGL 创建前关闭抗锯齿，针对 ThingJS 园区关闭具体后期特效、阴影和描边；保留模型、纹理、选中/点击拾取、标签和缩放所需的接口。不得将实际分辨率、像素比或 CPU 硬上限作为隐式降级手段。GPU 保持原效果；`original` 可恢复原效果。必须测量真实推流及操作结果，不能用简单测试页面帧率替代复杂园区验收，不能承诺未实测的 30/60 FPS。
* **CPU 输入和采集调度**：每个独立会话的 CPU 输入按序发送、最多 4 条等待回复，连续移动随真实画面更新只保留最新位置；滚轮保留逐次事件及总滚动量，按下、松开、键盘和文本事件保持顺序。采集持续推送有变化的原分辨率画面；无新帧时最多执行一个补充采集请求，不能反复重启采集或累积请求。无观看者时停止 JPEG 编码，保留场景运行和内存中的最新画面。Windows 可通过 `GOSTREAM3D_CPU_RENDERER=swiftshader|warp` 选择软件渲染后端，必须验证未使用硬件 GPU 渲染。
* **路数与交互隔离（必须）**：1 路是 1 个独立运行和推流的图形会话，只允许 1 位用户实时交互。10 路是 10 个独立会话配额，10 位用户各有独立画面、浏览器状态及输入，互不干扰。`max_viewers` 表示单链接最多占用的独立路数，而非一个 Worker 上的观看连接数；单路固定为 1，多路允许自定义 2–100，允许超过当前服务器总配额。项目配额与预加载路数分开设置。`preload_count` 控制提前准备的独立实例数（1–项目配额），旧记录默认 1，新建多路表单默认 3。新增观看者先独占一个已就绪 Worker、浏览器 Profile、信令与输入通道；没有就绪实例时才等待加载或按需启动。多路预加载池中的用户断开后，后台重新加载原始场景、补齐实例池，并复用原独立磁盘缓存。预加载实例占用服务器资源和总通道配额，禁止将一个实例分给两位用户。超过项目配额或服务器总配额时，新访问明确繁忙，不排队、不共享已有用户画面。CPU/GPU 项目及预加载实例全部共用服务器总配额；界面分别显示项目配额、总配额及已分配通道，不得宣传未验收的流畅并发能力。
* **服务器总通道设置（2026-10-02）**：默认 3 路由 HTTP/CDP/UDP 端口池可分配容量的最小值计算，不属于 GPU/硬件最大渲染路数的自动识别结果。管理员可在后台设置输入服务器总通道数 1–100，保存立即生效并持久化至会话数据目录的 `channel-settings.json`；启动时平台保存值优先于端口池初始容量。系统按原 HTTP/CDP/UDP 起始端口及每 Worker 的 UDP 数量调整末端。缩小值影响正在使用的实例或已分配端口时，须拒绝保存并保留旧值，不强停现有会话。管理员 `GET/PUT /api/v1/admin/channels/settings` 使用 `max_channels` 保存配额并返回 `active_channels`，前端保留草稿与生效值的区别。扩容只修改调度配额，实际可流畅并发需按场景、分辨率/帧率、GPU/CPU、显存、内存和网络实测；Windows JPEG/WebSocket 模式不存在 NVENC 最大通道测定。
* **平台监控**：管理员可查看运行节点的 CPU、内存、磁盘、网络速率、近一小时 CPU 使用率/内存使用率/网络趋势、磁盘分区、网络接口和系统信息。CPU 与内存使用率使用独立 ECharts 趋势图，纵轴为 0–100%，展示实际采样时间并可悬停查看数值；指标采集失败须保留缺口，不能记录成 0%。所有趋势复用现有采样周期，不增加后台采样任务；历史仅保存在服务器内存，最多保留近一小时且总点数有界，重启后重新积累。Windows/Linux 使用 Go `github.com/shirou/gopsutil/v4` 同一套采集接口；服务端默认每 30 秒采样并缓存，“后台设置”允许管理员配置 10–300 秒间隔且重启后保留。监控页面按该间隔读取缓存，API 需要管理员身份并支持手动刷新。监控指标不从观看者客户端采集。
* **平台 CPU 上限设置**：Windows 本机节点管理员可在“后台设置 → CPU 资源设置”修改本软件 CPU 使用硬上限（1–100%，默认 50%）。按整机 CPU 总处理能力计算，GoStream3D 服务与全部 CPU/GPU 渲染进程共用同一个进程组配额；保存后对现有及后续创建的渲染实例立即生效，无需重启。设置持久保存，启动时优先恢复平台值；读取失败、保存值无效或操作系统无法施加上限时不得假装保护成功。配置修改接口须验证管理员身份和整数范围，并让资源概况同步显示当前有效值。此限额约束本软件，不限制服务器上其他应用。Linux/Docker 未实现平台调整的节点须明确禁用设置并返回不支持，不能将进程优先级或监控阈值当作 CPU 硬配额。
* **运行形态**：Windows 本机 Worker 使用 Chromium 与 JPEG 画面流；Linux Worker 使用 Chromium/Neko 与 WebRTC。两种形态都必须满足相同的服务端预渲染和交互目标。下文旧版 Neko/Docker 示例属于历史方案，若与本节冲突，以本节和实际运行形态为准。
* **技术栈定位**：**Go (调度网关与容器编排) + React (极简无边框播放端) + Neko (沙箱渲染与流媒体底层)**
* **技术策略**：
* **站在巨人肩膀上**：利用 Neko 成熟稳定的 Xvfb 虚拟屏幕、GStreamer 编码、Pion WebRTC 媒体推流和虚拟输入注入能力，免去自己手写底层 C-Go/X11 绑定的复杂度。
* **裁剪与深度定制**：屏蔽 Neko 自带的复杂桌面协作外壳、登录面板与权限申请机制，做成**专用免密、全屏纯净、无感透传**的 3D 大屏流化网关。



```
[前端: 极简 React 页面] (访问专属短链 /Stream3D/:token)
         │
         ▼ (1. 鉴权免密请求与信令 WebSocket)
┌─────────────────────────────────────────────────────────────┐
│                 GoStream3D Gateway (Go 语言)                │
│                                                             │
│  - Token <-> 目标 3D 链接映射表                             │
│  - Docker SDK 容器池调度 (动态创建/启停 Neko 实例)          │
│  - 端口映射分配器 (WebRTC UDP 端口段、HTTP/WS 内部端口)     │
│  - 信令反向代理 (Signaling Reverse Proxy)                   │
│  - 空闲心跳检测与资源回收守护协程                           │
└──────────────────────────────┬──────────────────────────────┘
                               │ (2. 动态启动与代理)
                               ▼
┌─────────────────────────────────────────────────────────────┐
│               Neko Worker (定制化 Docker 容器)              │
│                                                             │
│   Chromium (Kiosk 独占 + SwiftShader/LLVMpipe 软解 3D)      │
│                            │                                │
│                            ▼                                │
│   Xvfb (虚拟屏幕) -> GStreamer (实时硬/软编码)              │
│                            │                                │
│                            ▼                                │
│   Neko 核心服务 (WebRTC 服务端 + 隐式控制权 Implicit Control)│
└──────────────────────────────┼──────────────────────────────┘
                               │
                               ▼ (3. WebRTC 视频流直出 + 键鼠控制)
[前端: 极简 React 页面] (<video> 纯净满屏渲染 + 鼠标拖拽旋转)

```

---

## 2. 完整技术选型清单

| 层次 / 模块 | 技术选型 | 说明与职责 |
| --- | --- | --- |
| **网关调度引擎** | **Go (Golang 1.22+) + Gin / Fiber** | 负责短链生成、Docker SDK 容器编排、WebSocket 信令隧道代理、超时回收引擎 |
| **容器编排接口** | **Docker Go SDK (`moby/moby/client`)** | 动态拉起、配置传参、生命周期管理与健康探测 |
| **底层 Worker 基础镜像** | **`m1k1o/neko:chromium`** | 内置 Xvfb、GStreamer、WebRTC 管道、Chromium 及输入事件回放 |
| **3D 软光栅化套件** | **Mesa `llvmpipe` / SwiftShader** | 服务端无独立 GPU 时，自动调用 CPU 多线程硬算 WebGL 着色器与光栅化 |
| **前端交互端** | **React 18 + Vite + TypeScript** | 极简播放视口，剥离原生 Neko 界面，仅保留全屏 `<video>` 与键鼠事件监听 |
| **信令通信** | **Gorilla WebSocket (`gorilla/websocket`)** | 在 Go 网关层充当双向中继代理（前端 React <-> 后端 Neko 内部 WS） |

---

## 3. 功能需求规范表 (FRD)

### 3.1 核心功能矩阵

| 需求编号 | 功能模块 | 详细技术需求 | 验收标准 |
| --- | --- | --- | --- |
| **FR-01** | **链接转换与免密生成** | 1. 提供 RESTful API 提交原 3D 页面 URL。<br>

<br>2. 系统生成唯一 NanoID/UUID 短链 `http://host:port/Stream3D/:token`。<br>

<br>3. 用户打开短链无需输入任何密码，直出画面。 | 浏览器打开短链，无登录弹窗、无外围桌面任务栏，直接呈现 3D 画布。 |
| **FR-02** | **服务端提前预渲染** | 1. 创建或启动链接时立即在服务端启动对应 Worker；程序重启后自动恢复未停止链接的预加载，不等第一位观众访问。<br>

<br>2. 服务端 Chromium 打开配置的原始 URL，加载脚本、模型和纹理，并在服务端完成 WebGL 场景渲染。<br>

<br>3. `document.readyState=complete` 和首帧不足以代表 3D 就绪；须等待场景专用就绪条件或管理员配置的就绪标记，并在条件满足后采集新帧。<br>

<br>4. 预加载失败应保留链接、展示错误并后台重试；已停止链接不得自动启动。 | 管理端只有在 3D 场景已加载并取得就绪后的新画面时显示“已预加载”。从该状态打开观看链接，客户端无需再等待模型/纹理的首次加载；本地网络下目标为 2 秒内收到已有画面。 |
| **FR-03** | **无感接管控制权 (Implicit Control)** | 1. 裁剪 Neko 默认的“申请控制权”逻辑。<br>

<br>2. 配置 Neko 环境变量，允许进入该流的用户默认且静默拥有鼠标键盘操控权。 | 用户鼠标放上去即可直接拖拽旋转 3D 场景，无须点击“Take Control”或请求权限。 |
| **FR-04** | **信令双向中继代理** | 1. 前端 React 连接 Go 网关暴露的 WebSocket。<br>

<br>2. Go 网关建立与对应 Neko 内部端口的连接，无损转发 WebRTC SDP Offer/Answer、ICE Candidate 及控制信令。 | 屏蔽 Neko 内部端口，客户端只需与 Go 网关单点通信即可打通音视频流。 |
| **FR-05** | **持续可交互与资源保护** | 1. 已预加载且未停止的链接必须保持服务端 3D 页面可见、可渲染，空闲优化不得让恢复后的页面永久处于 hidden 状态。<br>

<br>2. WebGL 场景即使未触发浏览器普通页面重绘，也要持续向观看者提供更新后的服务端画面；点击、拖动、滚轮和键盘输入须作用于同一服务端浏览器实例。<br>

<br>3. 观看页要保持与原页面一致的常见操作：鼠标拖拽离开画布后仍能正常释放，双击能触发原页面的双击事件，滚轮向上和向下都须传递带符号的位移并以合适的幅度缩放，触屏支持单指拖动和双指缩放；提供清晰的操作提示与全屏入口。<br>

<br>4. Windows 本机渲染必须维持整机 CPU 硬上限；内存和磁盘缓存用于保留场景及资源。管理员可单独停止链接以释放 Worker。 | 用可交互 3D 页面验证：服务端空闲后再访问，画面不是固定截图；拖动后客户端可看到新的画面；客户端不请求原始页面的 3D 资源，CPU 使用仍受配置上限约束。 |

---

## 4. 关键配置与参数规范

### 4.1 Neko Worker 容器启动参数规范（由 Go 调度器动态注入）

Go 调度器在调用 `docker.ContainerCreate` 时，必须注入以下环境变量与运行参数：

```go
// Go 容器创建配置
config := &container.Config{
    Image: "m1k1o/neko:chromium",
    Env: []string{
        // 1. 免密与权限：允许匿名，自动赋予交互权限，锁定单人操控
        "NEKO_PASSWORD_ADMIN=admin_internal_token",
        "NEKO_PASSWORD=user_internal_token",
        "NEKO_IMPLICIT_CONTROL=true",       // 核心：进场自动获得控制权
        "NEKO_LOCKS=control",

        // 2. 屏幕与视频流参数
        "NEKO_SCREEN=1920x1080@30",
        fmt.Sprintf("NEKO_WEBRTC_EPR=%d-%d", udpStartPort, udpEndPort), // 动态端口段

        // 3. Chromium 独占全屏与软解 3D 渲染
        fmt.Sprintf("NEKO_CHROME_ARGS=--kiosk --no-first-run --no-default-browser-check --disable-translate --use-gl=angle --use-angle=swiftshader --ignore-gpu-blocklist --enable-webgl --app=%s", targetURL),
    },
}

```

### 4.2 接口与信令协议规范

#### 1. 链接创建接口

* **Endpoint**: `POST /api/v1/sessions`
* **Request**:
```json
{
  "target_url": "https://threejs.org/examples/#webgl_animation_keyframes",
  "resolution": "1920x1080",
  "fps": 30
}

```


* **Response**:
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "g3d_9a8b7c6d",
    "stream_url": "http://192.168.1.100:8080/Stream3D/g3d_9a8b7c6d"
  }
}

```



#### 2. WebSocket 信令代理路由

* **Endpoint**: `GET /api/v1/ws/:token`
* **机制**：前端 React 连接此路由，Go 网关在接收到连接后，内部使用 `gorilla/websocket` 连接对应 Neko 容器的 `ws://<container_ip>:<internal_port>/api/ws?pwd=user_internal_token`，将信令原始帧透明双向转发。

---

## 5. 前后端核心模块实现规范

### 5.1 Go 网关信令中继代理实现规范 (`proxy.go`)

```go
package proxy

import (
	"fmt"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func ProxySignaling(c *gin.Context, nekoContainerIP string, nekoPort int, token string) {
	// 1. 升级客户端 WebSocket 连接
	clientConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 2. 连接 Neko 容器内部 WebSocket (带上免密参数)
	nekoWSURL := fmt.Sprintf("ws://%s:%d/api/ws?pwd=user_internal_token", nekoContainerIP, nekoPort)
	nekoConn, _, err := websocket.DefaultDialer.Dial(nekoWSURL, nil)
	if err != nil {
		return
	}
	defer nekoConn.Close()

	errChan := make(chan error, 2)

	// Client -> Neko 转发
	go func() {
		for {
			msgType, payload, err := clientConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := nekoConn.WriteMessage(msgType, payload); err != nil {
				errChan <- err
				return
			}
		}
	}()

	// Neko -> Client 转发
	go func() {
		for {
			msgType, payload, err := nekoConn.ReadMessage()
			if err != nil {
				errChan <- err
				return
			}
			if err := clientConn.WriteMessage(msgType, payload); err != nil {
				errChan <- err
				return
			}
		}
	}()

	<-errChan
}

```

### 5.2 React 纯净播放组件实现规范 (`PureStreamPlayer.tsx`)

直接复用 Neko 的客户端通信协议规范，但完全抛弃其 UI 组件，只渲染全屏 `<video>`：

```tsx
import React, { useEffect, useRef } from 'react';

interface Props {
  token: string;
}

export const PureStreamPlayer: React.FC<Props> = ({ token }) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);

  useEffect(() => {
    const pc = new RTCPeerConnection({
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
    });
    pcRef.current = pc;

    // 绑定远端流到 video
    pc.ontrack = (event) => {
      if (videoRef.current && event.streams[0]) {
        videoRef.current.srcObject = event.streams[0];
      }
    };

    // 连接 Go 网关的中继信令接口
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws/${token}`);

    ws.onmessage = async (e) => {
      const msg = JSON.parse(e.data);
      // 处理 Neko 的 WebRTC 握手信号
      if (msg.event === 'signal/offer') {
        await pc.setRemoteDescription(new RTCSessionDescription({ type: 'offer', sdp: msg.sdp }));
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        ws.send(JSON.stringify({ event: 'signal/answer', sdp: answer.sdp }));
      } else if (msg.event === 'signal/candidate') {
        await pc.addIceCandidate(new RTCIceCandidate(msg.data));
      }
    };

    pc.onicecandidate = (event) => {
      if (event.candidate) {
        ws.send(JSON.stringify({ event: 'signal/candidate', data: event.candidate }));
      }
    };

    return () => {
      pc.close();
      ws.close();
    };
  }, [token]);

  return (
    <video
      ref={videoRef}
      autoPlay
      playsInline
      muted
      style={{
        width: '100vw',
        height: '100vh',
        objectFit: 'fill',
        display: 'block',
        backgroundColor: '#000',
        cursor: 'default'
      }}
    />
  );
};

```

---

## 6. GoStream3D 项目目录规范

```text
GoStream3D/
├── cmd/
│   └── server/
│       └── main.go                 # 主入口：启动 Gin 网关与静态资源托管
├── internal/
│   ├── config/                     # 配置加载 (端口段范围、回收超时阈值、Docker 宿主机配置)
│   ├── api/                        # HTTP 接口与 WS 信令控制器
│   │   ├── session_handler.go      # 创建短链、查询活跃实例
│   │   └── router.go
│   ├── orchestrator/               # Docker 容器编排层
│   │   ├── docker_client.go        # Docker Go SDK 封装 (ContainerCreate/Start/Remove)
│   │   ├── port_allocator.go       # WebRTC UDP 端口范围分配器
│   │   └── session_manager.go      # 内存会话表、心跳更新与空闲自动回收巡检
│   └── proxy/                      # 信令中继层
│       └── websocket_proxy.go      # 双向转发前端 WS <-> 容器内 Neko WS
├── web/                            # React 18 + Vite 前端工程
│   ├── package.json
│   ├── vite.config.ts
│   └── src/
│       ├── App.tsx                 # 路由入口 (/Stream3D/:token)
│       └── components/
│           └── PureStreamPlayer.tsx# 纯净视频播放与交互事件透传组件
├── docker-compose.yml              # 快速部署示例
├── go.mod
└── go.sum

```

---

## 7. 实施计划与里程碑

```
阶段一：基础验证与通信打通 (Day 1)
  ├── 启动测试 Neko 容器，配置 --kiosk 和 NEKO_IMPLICIT_CONTROL
  └── 编写简易 HTML/React 验证 WebRTC 握手及自动获得操控权限

阶段二：Go 网关与信令代理开发 (Day 2)
  ├── 编写 Gin 路由与 Gorilla WebSocket 双向透传中间件
  └── 验证无需暴露 Neko 端口，只通过 Go 网关即可直接推流成功

阶段三：Docker SDK 动态生命周期管理 (Day 3)
  ├── 集成 moby/moby/client，实现根据短链动态拉起指定 URL 容器
  └── 实现 UDP 端口池分配机制与会话表维持

阶段四：自动清理与边界调优 (Day 4)
  ├── 实现定时检测心跳协程，连接断开满 3 分钟自动 docker rm -f 容器
  └── 针对 8C16G 宿主机进行并发压测，调整 CPU 资源硬限额参数

```
