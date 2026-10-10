# GoStream3D 基于 Neko 二次开发技术清单与规范需求表

---

## 1. 项目定位与二次开发架构

* **项目名称**：`GoStream3D`
* **当前产品目标**：3D 原始页面只在服务端浏览器加载并执行 WebGL；服务端提前完成场景资源加载与渲染，再把画面流送到低配置客户端。客户端只显示画面并回传输入，不下载模型/纹理或运行原页面的 3D 场景。预加载完成必须代表 3D 场景实际就绪，而非仅 HTML 加载或首帧到达。此目标优先于下文早期“访问时启动 Worker”的 Neko 方案。
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
| **FR-02** | **动态 Worker 编排与预热** | 1. 访问短链时，Go 网关检查若无可用容器，自动调用 Docker SDK 快速启动一个 Neko 实例。<br>

<br>2. 容器以环境变量形式注入目标 3D 链接，并开启 Chromium `--kiosk` 模式。<br>

<br>3. 动态分配独立的内部 Web 端口与 WebRTC UDP 端口范围。 | 启动过程对终端透明，稍候 2~3 秒后直接呈现已加载完的 3D 网页。 |
| **FR-03** | **无感接管控制权 (Implicit Control)** | 1. 裁剪 Neko 默认的“申请控制权”逻辑。<br>

<br>2. 配置 Neko 环境变量，允许进入该流的用户默认且静默拥有鼠标键盘操控权。 | 用户鼠标放上去即可直接拖拽旋转 3D 场景，无须点击“Take Control”或请求权限。 |
| **FR-04** | **信令双向中继代理** | 1. 前端 React 连接 Go 网关暴露的 WebSocket。<br>

<br>2. Go 网关建立与对应 Neko 内部端口的连接，无损转发 WebRTC SDP Offer/Answer、ICE Candidate 及控制信令。 | 屏蔽 Neko 内部端口，客户端只需与 Go 网关单点通信即可打通音视频流。 |
| **FR-05** | **空闲检测与容器自愈回收** | 1. React 端定时向 Go 网关上报心跳。<br>

<br>2. 用户关闭浏览器或断开连接持续超限（默认 180 秒），Go 后台协程自动调用 Docker API 执行 `Stop` 和 `Remove`，回收 CPU 和内存。 | 用户离开 3 分钟后，对应 Docker 容器彻底销毁，宿主机 CPU/内存恢复初始基线。 |

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

## 8. 管理控制台页面规范

控制台的布局、资源设置、项目操作、主题预览与响应式交互遵循 [云渲染控制台界面规范](../docs/ui-console.md)。设计参考图中的共享画面选项不适用于当前产品：1 路始终对应 1 位用户的独立交互会话，多路会话互不干扰。创建页场景插图仅作明确标识的示意，实际就绪状态以服务端返回为准。

